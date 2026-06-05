// Package app wires the gateway's dependency graph and owns its lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/sanjayrohith/redline/internal/api"
	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/config"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
	"github.com/sanjayrohith/redline/internal/health"
	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/inference"
	"github.com/sanjayrohith/redline/internal/logging"
	"github.com/sanjayrohith/redline/internal/metrics"
	"github.com/sanjayrohith/redline/internal/ratelimit"
	"github.com/sanjayrohith/redline/internal/redisclient"
	"github.com/sanjayrohith/redline/internal/router"
)

// App holds every dependency the gateway needs to serve traffic and owns
// the HTTP server's start/stop lifecycle.
type App struct {
	server        *http.Server
	metricsServer *http.Server
	logger        *slog.Logger
	dbPool        *db.Pool
	redis         *redisclient.Client
	repos         *db.Repositories
	sessions      *auth.SessionIssuer
	router        *router.Router
	metrics       *metrics.Registry
}

// New constructs the dependency graph and returns a ready-to-run App.
// Database connections are established lazily, so a currently-unreachable
// database does not prevent construction; only a malformed DSN does.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	logger := logging.New(cfg.LogLevel)

	rt := router.New(router.Config{
		Logger:      logger,
		Timeout:     cfg.RequestTimeout,
		CORSOrigins: cfg.CORSAllowedOrigins,
	})

	dbPool, err := db.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}

	redisClient := redisclient.NewClient(redisclient.Options{
		Addr:        cfg.RedisAddr,
		Password:    cfg.RedisPassword,
		DB:          cfg.RedisDB,
		PoolSize:    cfg.RedisPoolSize,
		MaxRetries:  cfg.RedisMaxRetries,
		DialTimeout: cfg.RedisDialTimeout,
	})

	repos := db.NewRepositories(dbPool)
	sessions := auth.NewSessionIssuer(auth.SessionConfig{
		SigningKey:      []byte(cfg.JWTSigningKey),
		Issuer:          cfg.JWTIssuer,
		Audience:        cfg.JWTAudience,
		AccessTokenTTL:  cfg.AccessTokenTTL,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
	}, repos.RefreshTokens)

	rt.Mux.HandleFunc("GET /healthz", health.LivenessHandler)
	rt.Mux.HandleFunc("GET /readyz", health.ReadinessHandler(health.NewAggregator(
		health.Check{Name: "postgres", Fn: dbPool.HealthCheck},
		health.Check{Name: "redis", Fn: redisClient.HealthCheck},
	)))

	metricsRegistry := metrics.NewRegistry()
	inferenceMetrics := metrics.NewInferenceCollectors(metricsRegistry)

	backend := inference.NewMockBackend(cfg.MockBackendTokenDelay)
	limiter := ratelimit.NewLimiter(redisClient)
	chatHandler := httpmw.APIKeyAuth(repos.APIKeys)(
		httpmw.RateLimit(limiter, cfg.ChatRateLimit, cfg.ChatRateLimitWindow, httpmw.PrincipalRouteKey("chat"))(
			api.ChatCompletionsHandler(backend, api.ChatCompletionsLimits{
				MaxSequenceLength: cfg.MaxSequenceLength,
				GenerationTimeout: cfg.GenerationTimeout,
				TTFT:              inferenceMetrics,
				TPOT:              inferenceMetrics,
				// MockBackend serves at no quantized precision - the
				// mock exists to exercise the request path in CI, not
				// to model a real deployment's precision choice.
				Quantization: "none",
			}),
		),
	)
	rt.Mux.Handle("POST /v1/chat/completions", chatHandler)

	rt.Mux.Handle("GET /v1/models", httpmw.APIKeyAuth(repos.APIKeys)(api.ModelsHandler(repos.Models)))

	authCookies := api.AuthCookieOptions{Secure: cfg.Environment != "development"}
	rt.Mux.Handle("POST /v1/auth/login", api.LoginHandler(repos.Users, sessions, authCookies))
	rt.Mux.Handle("POST /v1/auth/refresh", api.RefreshHandler(sessions, authCookies))
	rt.Mux.Handle("POST /v1/auth/logout", api.LogoutHandler(sessions, authCookies))

	sessionCfg := auth.SessionConfig{
		SigningKey: []byte(cfg.JWTSigningKey),
		Issuer:     cfg.JWTIssuer,
		Audience:   cfg.JWTAudience,
	}
	browserAuth := httpmw.JWTAuth(sessionCfg)
	rt.Mux.Handle("POST /v1/api-keys", browserAuth(api.CreateAPIKeyHandler(repos.APIKeys)))
	rt.Mux.Handle("GET /v1/api-keys", browserAuth(api.ListAPIKeysHandler(repos.APIKeys)))
	rt.Mux.Handle("DELETE /v1/api-keys/{id}", browserAuth(api.RevokeAPIKeyHandler(repos.APIKeys)))

	metricsMux := http.NewServeMux()
	metricsMux.Handle("GET /metrics", metricsRegistry.Handler())

	return &App{
		server: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           rt.Handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
		metricsServer: &http.Server{
			Addr:              cfg.MetricsListenAddr,
			Handler:           metricsMux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger:   logger,
		dbPool:   dbPool,
		redis:    redisClient,
		repos:    repos,
		sessions: sessions,
		router:   rt,
		metrics:  metricsRegistry,
	}, nil
}

// Metrics returns the App's metric collector registry, so packages
// outside app can register their own collectors against the same
// registry the /metrics endpoint serves, before Run starts.
func (a *App) Metrics() *metrics.Registry {
	return a.metrics
}

// Close releases resources held by the App, such as the database pool and
// the Redis client.
func (a *App) Close() {
	a.dbPool.Close()
	_ = a.redis.Close()
}

// Handler returns the fully wired HTTP handler - middleware chain and all
// registered routes - without binding a network listener. It exists so
// end-to-end tests can drive the real request path via httptest.
func (a *App) Handler() http.Handler {
	return a.server.Handler
}

// Repositories returns the App's typed repository layer, so tests can seed
// fixtures (users, API keys) directly against the same database the
// handler under test will query.
func (a *App) Repositories() *db.Repositories {
	return a.repos
}

// Migrate applies every pending database migration, guarded by a Postgres
// advisory lock so concurrent gateway replicas cannot race each other.
func (a *App) Migrate(ctx context.Context) error {
	pending, err := db.LoadMigrations(migrations.FS)
	if err != nil {
		return fmt.Errorf("app: load migrations: %w", err)
	}

	applied, err := db.NewMigrator(a.dbPool).Migrate(ctx, pending)
	if err != nil {
		return fmt.Errorf("app: apply migrations: %w", err)
	}

	a.logger.Info("migrations applied", "versions", applied)
	return nil
}

// Run starts the HTTP server and blocks until ctx is cancelled, at which
// point it gracefully shuts the server down.
func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.server.Addr, err)
	}

	metricsListener, err := net.Listen("tcp", a.metricsServer.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.metricsServer.Addr, err)
	}

	serveErr := make(chan error, 2)
	go func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()
	go func() {
		if err := a.metricsServer.Serve(metricsListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	a.logger.Info("gateway listening", "addr", a.server.Addr)
	a.logger.Info("metrics listening", "addr", a.metricsServer.Addr)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		a.logger.Info("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		if err := a.metricsServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("metrics shutdown: %w", err)
		}
		return nil
	}
}
