package app_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/sanjayrohith/redline/internal/app"
	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/config"
	"github.com/sanjayrohith/redline/internal/db"
)

// e2eEnv is the shared, real Postgres + Redis backing every contract test
// below, since the point of this suite is to exercise the actual request
// path - middleware chain, auth, rate limiting, the mock backend - not a
// stand-in for it.
type e2eEnv struct {
	postgresDSN string
	redisAddr   string
}

func setupE2EEnv(t *testing.T) e2eEnv {
	t.Helper()
	ctx := context.Background()

	pgContainer, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("redline_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
	)
	if err != nil {
		t.Skipf("skipping e2e test: could not start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = pgContainer.Terminate(context.Background()) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}

	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping e2e test: could not start redis container: %v", err)
	}
	t.Cleanup(func() { _ = redisContainer.Terminate(context.Background()) })

	redisURI, err := redisContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("redis connection string: %v", err)
	}
	redisAddr := strings.TrimPrefix(redisURI, "redis://")

	return e2eEnv{postgresDSN: dsn, redisAddr: redisAddr}
}

func waitReady(t *testing.T, a *app.App) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
		if rec.Code == http.StatusOK {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("app never became ready")
}

func newTestApp(t *testing.T, env e2eEnv, mutate func(*config.Config)) *app.App {
	t.Helper()

	cfg := &config.Config{
		ListenAddr:        "127.0.0.1:0",
		MetricsListenAddr: "127.0.0.1:0",
		Environment:       "test",
		LogLevel:          "error",
		DatabaseURL:       env.postgresDSN,
		DBMaxConns:        5,
		DBConnectTimeout:  10 * time.Second,

		JWTSigningKey:   "test-signing-key",
		JWTIssuer:       "redline",
		JWTAudience:     "redline-api",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,

		RedisAddr:        env.redisAddr,
		RedisPoolSize:    10,
		RedisMaxRetries:  3,
		RedisDialTimeout: 5 * time.Second,

		RequestTimeout: 5 * time.Second,

		ChatRateLimit:       100,
		ChatRateLimitWindow: time.Minute,

		MaxSequenceLength: 8192,
		GenerationTimeout: 5 * time.Second,
	}
	if mutate != nil {
		mutate(cfg)
	}

	a, err := app.New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("app.New() error = %v", err)
	}
	t.Cleanup(a.Close)

	waitDBHealthy(t, a)

	if err := a.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	waitReady(t, a)
	return a
}

// waitDBHealthy retries a trivial query before the very first migration
// attempt, since a fresh Postgres container's readiness log line can land
// a moment before it actually accepts TCP connections (it restarts once
// internally after initdb). Any response at all - including the
// "relation does not exist" error expected pre-migration - proves the
// connection itself succeeded; only a connection-level failure is retried.
func waitDBHealthy(t *testing.T, a *app.App) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		_, lastErr = a.Repositories().Users.GetByID(context.Background(), "00000000-0000-0000-0000-000000000000")
		if !isConnectionError(lastErr) {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("postgres never accepted a connection: %v", lastErr)
}

func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "failed to connect") ||
		strings.Contains(msg, "connection refused")
}

// issueAPIKey creates a user and an active API key with the given scopes,
// returning the plaintext key a test can send as a bearer credential.
func issueAPIKey(t *testing.T, a *app.App, email string, scopes []string) string {
	t.Helper()
	ctx := context.Background()

	user, err := a.Repositories().Users.Create(ctx, email)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	generated, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	if _, err := a.Repositories().APIKeys.Create(ctx, db.NewAPIKey{
		UserID:        user.ID,
		KeyHash:       generated.Hash,
		DisplayPrefix: generated.DisplayPrefix,
		Scopes:        scopes,
	}); err != nil {
		t.Fatalf("create api key: %v", err)
	}

	return generated.Plaintext
}

func decodeErrorEnvelope(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error envelope: %v (body: %s)", err, rec.Body.String())
	}
	return body.Error.Code
}

func chatRequest(t *testing.T, key, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req
}

func TestE2E_ChatCompletions_SuccessPath(t *testing.T) {
	env := setupE2EEnv(t)
	a := newTestApp(t, env, nil)
	key := issueAPIKey(t, a, "success@example.com", []string{"inference"})

	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, chatRequest(t, key, `{"model":"mock-model","messages":[{"role":"user","content":"hello"}]}`))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Object  string `json:"object"`
		Choices []struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want chat.completion", resp.Object)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Role != "assistant" || resp.Choices[0].FinishReason != "stop" {
		t.Errorf("Choices = %+v", resp.Choices)
	}
}

func TestE2E_ChatCompletions_AuthFailure(t *testing.T) {
	env := setupE2EEnv(t)
	a := newTestApp(t, env, nil)

	cases := []struct {
		name string
		key  string
	}{
		{"missing credential", ""},
		{"unknown key", "rl_totally-unknown-key"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.Handler().ServeHTTP(rec, chatRequest(t, tc.key, `{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}`))

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := decodeErrorEnvelope(t, rec); got != "unauthorized" {
				t.Errorf("error.code = %q, want unauthorized", got)
			}
		})
	}
}

func TestE2E_ChatCompletions_ValidationFailure(t *testing.T) {
	env := setupE2EEnv(t)
	a := newTestApp(t, env, nil)
	key := issueAPIKey(t, a, "validation@example.com", []string{"inference"})

	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, chatRequest(t, key, `{"model":"","messages":[]}`))

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeErrorEnvelope(t, rec); got != "validation_error" {
		t.Errorf("error.code = %q, want validation_error", got)
	}
}

func TestE2E_ChatCompletions_RateLimited(t *testing.T) {
	env := setupE2EEnv(t)
	a := newTestApp(t, env, func(cfg *config.Config) {
		cfg.ChatRateLimit = 2
		cfg.ChatRateLimitWindow = time.Minute
	})
	key := issueAPIKey(t, a, "ratelimit@example.com", []string{"inference"})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}`

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		a.Handler().ServeHTTP(rec, chatRequest(t, key, body))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200, body = %s", i, rec.Code, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, chatRequest(t, key, body))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeErrorEnvelope(t, rec); got != "rate_limited" {
		t.Errorf("error.code = %q, want rate_limited", got)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header should be set")
	}
}

func TestE2E_ChatCompletions_GenerationTimeout(t *testing.T) {
	env := setupE2EEnv(t)
	a := newTestApp(t, env, func(cfg *config.Config) {
		cfg.GenerationTimeout = 10 * time.Millisecond
		cfg.MockBackendTokenDelay = 200 * time.Millisecond
	})
	key := issueAPIKey(t, a, "timeout@example.com", []string{"inference"})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"this response needs several tokens to generate fully"}]}`

	rec := httptest.NewRecorder()
	a.Handler().ServeHTTP(rec, chatRequest(t, key, body))

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504, body = %s", rec.Code, rec.Body.String())
	}
	if got := decodeErrorEnvelope(t, rec); got != "request_timeout" {
		t.Errorf("error.code = %q, want request_timeout", got)
	}
}
