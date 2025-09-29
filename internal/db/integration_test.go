package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
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

	waitForHealthy(ctx, t, pool)

	pending, err := db.LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if _, err := db.NewMigrator(pool).Migrate(ctx, pending); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return pool
}

// waitForHealthy retries the health check briefly, since the container's
// readiness log line can land a moment before it actually accepts TCP
// connections.
func waitForHealthy(ctx context.Context, t *testing.T, pool *db.Pool) {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		lastErr = pool.HealthCheck(checkCtx)
		cancel()
		if lastErr == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("postgres never became healthy: %v", lastErr)
}

func TestRepositories_RoundTrip(t *testing.T) {
	pool := newTestPool(t)
	repos := db.NewRepositories(pool)
	ctx := context.Background()

	t.Run("user create get and duplicate email conflict", func(t *testing.T) {
		u, err := repos.Users.Create(ctx, "alice@example.com")
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		got, err := repos.Users.GetByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if got.Email != "alice@example.com" {
			t.Errorf("Email = %q, want alice@example.com", got.Email)
		}

		if _, err := repos.Users.Create(ctx, "alice@example.com"); err != db.ErrConflict {
			t.Errorf("Create() duplicate error = %v, want ErrConflict", err)
		}

		if _, err := repos.Users.GetByID(ctx, "00000000-0000-0000-0000-000000000000"); err != db.ErrNotFound {
			t.Errorf("GetByID() missing error = %v, want ErrNotFound", err)
		}
	})

	t.Run("org create list", func(t *testing.T) {
		before, err := repos.Orgs.List(ctx)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}

		if _, err := repos.Orgs.Create(ctx, "acme"); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		after, err := repos.Orgs.List(ctx)
		if err != nil {
			t.Fatalf("List() error = %v", err)
		}
		if len(after) != len(before)+1 {
			t.Errorf("len(after) = %d, want %d", len(after), len(before)+1)
		}
	})

	t.Run("model deployment inference run and telemetry chain", func(t *testing.T) {
		u, err := repos.Users.Create(ctx, "bob@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		m, err := repos.Models.Create(ctx, db.NewModel{
			RepoURL: "hf://bob/model", Revision: "main", Architecture: "llama",
			ParameterCount: 7_000_000_000, Dtype: "fp16", VRAMEstimateFP16Bytes: 14_000_000_000,
		})
		if err != nil {
			t.Fatalf("create model: %v", err)
		}

		if _, err := repos.Models.Create(ctx, db.NewModel{
			RepoURL: "hf://bob/model", Revision: "main", Architecture: "llama",
			ParameterCount: 7_000_000_000, Dtype: "fp16", VRAMEstimateFP16Bytes: 14_000_000_000,
		}); err != db.ErrConflict {
			t.Errorf("duplicate model Create() error = %v, want ErrConflict", err)
		}

		dep, err := repos.Deployments.Create(ctx, m.ID)
		if err != nil {
			t.Fatalf("create deployment: %v", err)
		}
		if dep.State != "queued" {
			t.Errorf("State = %q, want queued", dep.State)
		}

		if err := repos.Deployments.UpdateState(ctx, dep.ID, "ready"); err != nil {
			t.Fatalf("UpdateState() error = %v", err)
		}
		updated, err := repos.Deployments.GetByID(ctx, dep.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if updated.State != "ready" {
			t.Errorf("State = %q, want ready", updated.State)
		}

		run, err := repos.InferenceRuns.Create(ctx, dep.ID, m.ID, u.ID)
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		if run.CompletedAt != nil {
			t.Error("CompletedAt should be nil for a fresh run")
		}

		ttft := 42.5
		if _, err := repos.TelemetrySamples.Create(ctx, db.NewTelemetrySample{
			InferenceRunID: run.ID, TTFTMs: &ttft,
		}); err != nil {
			t.Fatalf("create sample: %v", err)
		}

		samples, err := repos.TelemetrySamples.ListByRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("ListByRun() error = %v", err)
		}
		if len(samples) != 1 {
			t.Fatalf("len(samples) = %d, want 1", len(samples))
		}
		if samples[0].TPOTMs != nil {
			t.Error("TPOTMs should be nil, it was never set")
		}

		if err := repos.InferenceRuns.Complete(ctx, run.ID, 10, 20); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		completed, err := repos.InferenceRuns.GetByID(ctx, run.ID)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if completed.CompletedAt == nil {
			t.Error("CompletedAt should be set after Complete()")
		}
		if completed.PromptTokens != 10 || completed.CompletionTokens != 20 {
			t.Errorf("tokens = (%d, %d), want (10, 20)", completed.PromptTokens, completed.CompletionTokens)
		}
	})

	t.Run("api key create touch revoke", func(t *testing.T) {
		u, err := repos.Users.Create(ctx, "carol@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		key, err := repos.APIKeys.Create(ctx, db.NewAPIKey{
			UserID: u.ID, KeyHash: "hashed", DisplayPrefix: "rl_carol", Scopes: []string{"inference"},
		})
		if err != nil {
			t.Fatalf("create key: %v", err)
		}

		found, err := repos.APIKeys.GetByPrefix(ctx, "rl_carol")
		if err != nil {
			t.Fatalf("GetByPrefix() error = %v", err)
		}
		if found.ID != key.ID {
			t.Errorf("found.ID = %q, want %q", found.ID, key.ID)
		}

		if err := repos.APIKeys.TouchLastUsed(ctx, key.ID); err != nil {
			t.Fatalf("TouchLastUsed() error = %v", err)
		}

		if err := repos.APIKeys.Revoke(ctx, key.ID); err != nil {
			t.Fatalf("Revoke() error = %v", err)
		}
		if err := repos.APIKeys.Revoke(ctx, key.ID); err != db.ErrNotFound {
			t.Errorf("re-Revoke() error = %v, want ErrNotFound", err)
		}

		keys, err := repos.APIKeys.ListByUser(ctx, u.ID)
		if err != nil {
			t.Fatalf("ListByUser() error = %v", err)
		}
		if len(keys) != 1 || keys[0].RevokedAt == nil {
			t.Errorf("ListByUser() = %+v, want one revoked key", keys)
		}
	})
}
