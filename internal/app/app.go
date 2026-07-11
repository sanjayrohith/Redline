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

	nomadAPI "github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/api"
	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/bench"
	"github.com/sanjayrohith/redline/internal/billing"
	"github.com/sanjayrohith/redline/internal/config"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
	"github.com/sanjayrohith/redline/internal/health"
	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/inference"
	"github.com/sanjayrohith/redline/internal/ingest"
	"github.com/sanjayrohith/redline/internal/logging"
	"github.com/sanjayrohith/redline/internal/metrics"
	"github.com/sanjayrohith/redline/internal/nomadclient"
	"github.com/sanjayrohith/redline/internal/policy"
	"github.com/sanjayrohith/redline/internal/queue"
	"github.com/sanjayrohith/redline/internal/ratelimit"
	"github.com/sanjayrohith/redline/internal/redisclient"
	"github.com/sanjayrohith/redline/internal/resilience"
	"github.com/sanjayrohith/redline/internal/router"
	"github.com/sanjayrohith/redline/internal/scheduler"
	"github.com/sanjayrohith/redline/internal/telemetry"
)

const ingestionQueueName = "ingestion"
const retryQueueName = "inference-retry"
const deploymentRetryQueueName = "deployment-retry"

// defaultInferenceImage is the container image every deployment's Nomad
// job runs, pending a per-model or per-precision image selection this
// gateway does not yet make.
const defaultInferenceImage = "redline/vllm:latest"

// stuckRequestThreshold bounds the gap between two consecutive tokens of
// a streamed generation before resilience.Watchdog considers it stuck.
const stuckRequestThreshold = 20 * time.Second

// stuckSweepInterval is how often the stuck-request reaper sweeps for
// requests past stuckRequestThreshold.
const stuckSweepInterval = 5 * time.Second

// defaultGPUHourlyRates is a placeholder on-demand pricing table, pending
// an ops-configured source of truth (a config field or pricing service).
// Values are illustrative list prices for the named GPU model, in USD per
// hour of allocation wall time.
var defaultGPUHourlyRates = billing.HourlyRates{
	"A100": 2.50,
	"H100": 4.50,
	"L40S": 1.80,
}

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
	ingestionPool *queue.WorkerPool
	telemetryHub  *telemetry.Hub
	stuckReaper   *resilience.Reaper
}

// gpuSampleInterval is how often the GPU utilization/VRAM gauges refresh
// over the telemetry WebSocket.
const gpuSampleInterval = 5 * time.Second

// New constructs the dependency graph and returns a ready-to-run App.
// Database connections are established lazily, so a currently-unreachable
// database does not prevent construction; only a malformed DSN does.
func New(ctx context.Context, cfg *config.Config) (*App, error) {
	logger := logging.New(cfg.LogLevel)

	rt := router.New(router.Config{
		Logger:      logger,
		Timeout:     cfg.RequestTimeout,
		CORSOrigins: cfg.CORSAllowedOrigins,
		// Streaming SSE responses need a real http.Flusher, which the
		// Timeout middleware's default response-wrapping does not
		// provide - see httpmw.Timeout. Both chat completion routes
		// (API-key and dashboard-session authed) can stream.
		TimeoutExemptPrefixes: []string{"/v1/chat/completions", "/v1/dashboard/chat/completions"},
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
	telemetryHub := telemetry.NewHub()
	telemetryPublisher := telemetry.NewPublisher(telemetryHub, inferenceMetrics)

	backend := inference.NewMockBackend(cfg.MockBackendTokenDelay)
	limiter := ratelimit.NewLimiter(redisClient)

	// stuckWatchdog detects a request that has stopped producing tokens
	// mid-stream - something GenerationTimeout alone cannot, since a
	// generation that emits its first tokens promptly and then hangs
	// still has time left on that deadline. stuckRequestThreshold is
	// deliberately much shorter than a typical GenerationTimeout: it
	// bounds the gap between tokens, not the whole generation.
	stuckWatchdog := resilience.NewWatchdog(stuckRequestThreshold)
	retryQueue := queue.New(redisClient, retryQueueName, queue.Options{})
	stuckReaper := resilience.NewReaper(stuckWatchdog, backend, retryQueue)

	chatHandler := resilience.Middleware(stuckWatchdog)(
		httpmw.APIKeyAuth(repos.APIKeys)(
			httpmw.RateLimit(limiter, cfg.ChatRateLimit, cfg.ChatRateLimitWindow, httpmw.PrincipalRouteKey("chat"))(
				api.ChatCompletionsHandler(backend, api.ChatCompletionsLimits{
					MaxSequenceLength: cfg.MaxSequenceLength,
					GenerationTimeout: cfg.GenerationTimeout,
					TTFT:              telemetryPublisher,
					TPOT:              telemetryPublisher,
					Watchdog:          stuckWatchdog,
					// MockBackend serves at no quantized precision - the
					// mock exists to exercise the request path in CI, not
					// to model a real deployment's precision choice.
					Quantization: "none",
				}),
			),
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

	// Session-authed mirrors of the model catalog, for the dashboard's own
	// pages - which carry a browser session cookie, not an API key.
	rt.Mux.Handle("GET /v1/dashboard/models", browserAuth(api.ModelCatalogHandler(repos.Models)))
	rt.Mux.Handle("GET /v1/dashboard/models/{id}", browserAuth(api.ModelDetailHandler(repos.Models)))
	rt.Mux.Handle("GET /v1/dashboard/deployments/{id}/session",
		browserAuth(api.SessionCostHandler(repos.Deployments, defaultGPUHourlyRates, cfg.IdleTimeout)))

	// Session-authed mirror of chat completions, so the dashboard's
	// playground can stream generations against the same backend an API
	// key client uses, without the browser ever holding an API key.
	rt.Mux.Handle("POST /v1/dashboard/chat/completions",
		resilience.Middleware(stuckWatchdog)(
			browserAuth(httpmw.RateLimit(limiter, cfg.ChatRateLimit, cfg.ChatRateLimitWindow, httpmw.PrincipalRouteKey("dashboard-chat"))(
				api.ChatCompletionsHandler(backend, api.ChatCompletionsLimits{
					MaxSequenceLength: cfg.MaxSequenceLength,
					GenerationTimeout: cfg.GenerationTimeout,
					TTFT:              telemetryPublisher,
					TPOT:              telemetryPublisher,
					Watchdog:          stuckWatchdog,
					Quantization:      "none",
				}),
			)),
		),
	)

	rt.Mux.Handle("GET /v1/dashboard/ws/telemetry", browserAuth(telemetry.Handler(telemetryHub)))

	rt.Mux.Handle("POST /v1/dashboard/benchmarks",
		browserAuth(api.CreateBenchmarkRunHandler(repos.Deployments, repos.BenchmarkRuns, backend, bench.Run)))
	rt.Mux.Handle("GET /v1/dashboard/benchmarks", browserAuth(api.ListBenchmarkRunsHandler(repos.BenchmarkRuns)))

	ingestionQueue := queue.New(redisClient, ingestionQueueName, queue.Options{})
	rt.Mux.Handle("POST /v1/ingestions", browserAuth(api.CreateIngestionHandler(repos.IngestionJobs, ingestionQueue)))
	rt.Mux.Handle("GET /v1/ingestions/{id}", browserAuth(api.GetIngestionHandler(repos.IngestionJobs)))

	manifestClient := ingest.NewClient()
	orchestrator := ingest.NewOrchestrator(
		manifestClient,
		ingest.NewHTTPRangeFetcher(nil),
		repos.IngestionJobs,
		repos.Models,
		repos.Models,
		ingest.DefaultQuota,
	)
	ingestionPool := queue.NewWorkerPool(ingestionQueue, ingestionJobHandler(repos.IngestionJobs, orchestrator), 4, logger)

	nomadClient, err := nomadclient.NewClient(cfg.NomadAddr)
	if err != nil {
		return nil, fmt.Errorf("app: %w", err)
	}
	deploymentRetryQueue := queue.New(redisClient, deploymentRetryQueueName, queue.Options{})
	degradedDispatcher := scheduler.NewDegradedModeDispatcher(nomadClient, deploymentRetryQueue)
	buildJob := func(deploymentID string, model *db.Model) *nomadAPI.Job {
		return nomadclient.BuildInferenceJob(nomadclient.InferenceJobSpec{
			DeploymentID: deploymentID,
			Image:        defaultInferenceImage,
			MinVRAMBytes: model.VRAMEstimateFP16Bytes,
		})
	}
	rt.Mux.Handle("POST /v1/dashboard/deployments",
		browserAuth(api.CreateDeploymentHandler(repos.Deployments, repos.Models, degradedDispatcher, buildJob)))
	rt.Mux.Handle("GET /v1/dashboard/scheduler/status", browserAuth(api.SchedulerStatusHandler(degradedDispatcher)))

	rt.Mux.Handle("POST /v1/dashboard/tos/accept", browserAuth(api.AcceptTOSHandler(repos.Users)))

	suspensionEnforcer := policy.NewEnforcer(repos.Users, repos.APIKeys, repos.Deployments, nomadClient, nomadclient.InferenceJobID)
	rt.Mux.Handle("POST /v1/admin/users/{id}/suspend",
		httpmw.APIKeyAuth(repos.APIKeys)(httpmw.RequireScope("admin")(api.SuspendUserHandler(suspensionEnforcer))))

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
		logger:        logger,
		dbPool:        dbPool,
		redis:         redisClient,
		repos:         repos,
		sessions:      sessions,
		router:        rt,
		metrics:       metricsRegistry,
		ingestionPool: ingestionPool,
		telemetryHub:  telemetryHub,
		stuckReaper:   stuckReaper,
	}, nil
}

// sampleGPUs adapts metrics.SampleGPUs to telemetry.GPUSampleFunc.
func sampleGPUs(ctx context.Context) ([]telemetry.GPUSample, error) {
	readings, err := metrics.SampleGPUs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]telemetry.GPUSample, len(readings))
	for i, r := range readings {
		out[i] = telemetry.GPUSample{
			DeviceIndex: r.DeviceIndex, UtilizationPercent: r.UtilizationPercent,
			VRAMUsedBytes: r.VRAMUsedBytes, VRAMTotalBytes: r.VRAMTotalBytes,
		}
	}
	return out, nil
}

// ingestionJobHandler adapts Orchestrator.Run to queue.Handler: a job's
// payload is its own id, since the durable state ingestion needs already
// lives in the ingestion_jobs row rather than the queue payload.
func ingestionJobHandler(jobs *db.IngestionJobRepository, orchestrator *ingest.Orchestrator) queue.Handler {
	return func(ctx context.Context, job *queue.Job) error {
		record, err := jobs.GetByID(ctx, job.Payload)
		if err != nil {
			return fmt.Errorf("app: look up ingestion job %s: %w", job.Payload, err)
		}

		ref, err := ingest.ParseReference(record.RepoURL)
		if err != nil {
			_ = jobs.Fail(ctx, record.ID, err.Error())
			return nil
		}
		ref.Revision = record.Revision

		return orchestrator.Run(ctx, record.ID, *ref)
	}
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
	go a.ingestionPool.Run(ctx)
	go telemetry.PublishGPUSamples(ctx, a.telemetryHub, sampleGPUs, gpuSampleInterval)
	go a.stuckReaper.Run(ctx, stuckSweepInterval)

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
