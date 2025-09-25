package httpmw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

func newTestPool(t *testing.T) *db.Pool {
	t.Helper()

	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("redline_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
	)
	if err != nil {
		t.Skipf("skipping integration test: could not start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	pool, err := db.NewPool(ctx, dsn, 5, 10*time.Second)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		lastErr = pool.HealthCheck(checkCtx)
		cancel()
		if lastErr == nil {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("postgres never became healthy: %v", lastErr)
	}

	pending, err := db.LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if _, err := db.NewMigrator(pool).Migrate(ctx, pending); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return pool
}

// TestAPIKeyRevocation_TakesEffectImmediately proves that revoking a key
// through the repository is visible to the very next authenticated
// request - there is no caching layer that could serve a stale, still-usable
// credential after revocation.
func TestAPIKeyRevocation_TakesEffectImmediately(t *testing.T) {
	pool := newTestPool(t)
	repos := db.NewRepositories(pool)
	ctx := context.Background()

	user, err := repos.Users.Create(ctx, "revoke-me@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	generated, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	key, err := repos.APIKeys.Create(ctx, db.NewAPIKey{
		UserID:        user.ID,
		KeyHash:       generated.Hash,
		DisplayPrefix: generated.DisplayPrefix,
		Scopes:        []string{"inference"},
	})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	handler := httpmw.APIKeyAuth(repos.APIKeys)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Authorization", "Bearer "+generated.Plaintext)
		return r
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusOK {
		t.Fatalf("before revocation: status = %d, want 200", rec.Code)
	}

	if err := repos.APIKeys.Revoke(ctx, key.ID); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req())
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("after revocation: status = %d, want 401", rec.Code)
	}
}
