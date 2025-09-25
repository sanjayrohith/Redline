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

	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/config"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
	"github.com/sanjayrohith/redline/internal/logging"
	"github.com/sanjayrohith/redline/internal/redisclient"
	"github.com/sanjayrohith/redline/internal/router"
)

// App holds every dependency the gateway needs to serve traffic and owns
// the HTTP server's start/stop lifecycle.
type App struct {
	server   *http.Server
	logger   *slog.Logger
	dbPool   *db.Pool
	redis    *redisclient.Client
	repos    *db.Repositories
	sessions *auth.SessionIssuer
	router   *router.Router
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

	return &App{
		server: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           rt.Handler,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger:   logger,
		dbPool:   dbPool,
		redis:    redisClient,
		repos:    repos,
		sessions: sessions,
		router:   rt,
	}, nil
}

// Close releases resources held by the App, such as the database pool and
// the Redis client.
func (a *App) Close() {
	a.dbPool.Close()
	_ = a.redis.Close()
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

	serveErr := make(chan error, 1)
	go func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	a.logger.Info("gateway listening", "addr", a.server.Addr)

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
		return nil
	}
}
