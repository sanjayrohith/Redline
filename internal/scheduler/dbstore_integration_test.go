package scheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

func TestDBDeploymentStore_CurrentStateAndUpdateState(t *testing.T) {
	ctx := context.Background()

	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("redline_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
	)
	if err != nil {
		t.Skipf("skipping integration test: could not start postgres container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

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

	repos := db.NewRepositories(pool)

	model, err := repos.Models.Create(ctx, db.NewModel{
		RepoURL: "hf://org/model", Revision: "main", Architecture: "llama",
		ParameterCount: 1, Dtype: "fp16",
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	deployment, err := repos.Deployments.Create(ctx, model.ID)
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	store := &scheduler.DBDeploymentStore{Repo: repos.Deployments}

	current, err := store.CurrentState(ctx, deployment.ID)
	if err != nil {
		t.Fatalf("CurrentState() error = %v", err)
	}
	if current != scheduler.StateQueued {
		t.Errorf("CurrentState() = %q, want queued (the default for a freshly created deployment)", current)
	}

	if err := scheduler.ValidateTransition(current, scheduler.StateProvisioning); err != nil {
		t.Fatalf("ValidateTransition() error = %v", err)
	}
	if err := store.UpdateState(ctx, deployment.ID, scheduler.StateProvisioning); err != nil {
		t.Fatalf("UpdateState() error = %v", err)
	}

	updated, err := store.CurrentState(ctx, deployment.ID)
	if err != nil {
		t.Fatalf("CurrentState() error = %v", err)
	}
	if updated != scheduler.StateProvisioning {
		t.Errorf("CurrentState() = %q, want provisioning", updated)
	}
}
