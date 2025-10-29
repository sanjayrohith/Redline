package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
)

func TestTelemetryRollupRepository_ComputeModelRollups(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	pending, err := db.LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if _, err := db.NewMigrator(pool).Migrate(ctx, pending); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	repos := db.NewRepositories(pool)

	user, err := repos.Users.Create(ctx, "rollup-test@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	model, err := repos.Models.Create(ctx, db.NewModel{
		RepoURL: "org/rollup-model", Revision: "main", Architecture: "llama",
		ParameterCount: 7_000_000_000, Dtype: "fp16",
		VRAMEstimateFP16Bytes: 14 << 30, VRAMEstimateFP8Bytes: 7 << 30, VRAMEstimateInt4Bytes: 4 << 30,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	deployment, err := repos.Deployments.Create(ctx, model.ID)
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	if err := repos.Deployments.SetGPUModel(ctx, deployment.ID, "A100"); err != nil {
		t.Fatalf("SetGPUModel() error = %v", err)
	}

	ttftValues := []float64{10, 20, 30, 40, 50}
	for _, ttft := range ttftValues {
		run, err := repos.InferenceRuns.Create(ctx, deployment.ID, model.ID, user.ID, "alloc-1")
		if err != nil {
			t.Fatalf("create run: %v", err)
		}
		tpot := ttft // same distribution for simplicity
		vramPeak := int64(10 << 30)
		cost := 0.01
		if err := repos.InferenceRuns.CompleteWithTelemetry(ctx, run.ID, db.RunTelemetry{
			PromptTokens: 5, CompletionTokens: 10,
			TTFTMs: &ttft, TPOTMs: &tpot, VRAMPeakBytes: &vramPeak, CostUSD: &cost,
		}); err != nil {
			t.Fatalf("CompleteWithTelemetry() error = %v", err)
		}
	}

	rollups, err := repos.TelemetryRollups.ComputeModelRollups(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ComputeModelRollups() error = %v", err)
	}
	if len(rollups) != 1 {
		t.Fatalf("len(rollups) = %d, want 1", len(rollups))
	}

	r := rollups[0]
	if r.ModelID != model.ID {
		t.Errorf("ModelID = %q, want %q", r.ModelID, model.ID)
	}
	if r.GPUModel != "A100" {
		t.Errorf("GPUModel = %q, want A100", r.GPUModel)
	}
	if r.RunCount != 5 {
		t.Errorf("RunCount = %d, want 5", r.RunCount)
	}
	if r.TTFTP50Ms == nil || *r.TTFTP50Ms != 30 {
		t.Errorf("TTFTP50Ms = %v, want 30 (median of 10,20,30,40,50)", r.TTFTP50Ms)
	}
	if r.TTFTP95Ms == nil || *r.TTFTP95Ms < *r.TTFTP50Ms {
		t.Errorf("TTFTP95Ms = %v, want >= TTFTP50Ms (%v)", r.TTFTP95Ms, r.TTFTP50Ms)
	}
	if r.TTFTP99Ms == nil || *r.TTFTP99Ms < *r.TTFTP95Ms {
		t.Errorf("TTFTP99Ms = %v, want >= TTFTP95Ms (%v)", r.TTFTP99Ms, r.TTFTP95Ms)
	}
	if r.ThroughputTokensPerSec == nil || *r.ThroughputTokensPerSec <= 0 {
		t.Errorf("ThroughputTokensPerSec = %v, want > 0", r.ThroughputTokensPerSec)
	}
	if r.TotalCostUSD == nil || (*r.TotalCostUSD-0.05) > 1e-9 || (0.05-*r.TotalCostUSD) > 1e-9 {
		t.Errorf("TotalCostUSD = %v, want ~0.05 (5 runs at $0.01 each)", r.TotalCostUSD)
	}
}

func TestTelemetryRollupRepository_ExcludesRunsOutsideTheWindow(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()

	pending, err := db.LoadMigrations(migrations.FS)
	if err != nil {
		t.Fatalf("LoadMigrations() error = %v", err)
	}
	if _, err := db.NewMigrator(pool).Migrate(ctx, pending); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	repos := db.NewRepositories(pool)

	user, err := repos.Users.Create(ctx, "windowed@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	model, err := repos.Models.Create(ctx, db.NewModel{
		RepoURL: "org/windowed-model", Revision: "main", Architecture: "llama",
		ParameterCount: 1, Dtype: "fp16",
		VRAMEstimateFP16Bytes: 1, VRAMEstimateFP8Bytes: 1, VRAMEstimateInt4Bytes: 1,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	deployment, err := repos.Deployments.Create(ctx, model.ID)
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	run, err := repos.InferenceRuns.Create(ctx, deployment.ID, model.ID, user.ID, "alloc-1")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	ttft := 15.0
	if err := repos.InferenceRuns.CompleteWithTelemetry(ctx, run.ID, db.RunTelemetry{
		PromptTokens: 1, CompletionTokens: 1, TTFTMs: &ttft,
	}); err != nil {
		t.Fatalf("CompleteWithTelemetry() error = %v", err)
	}

	// A zero-width window excludes even a run completed moments ago.
	rollups, err := repos.TelemetryRollups.ComputeModelRollups(ctx, 0)
	if err != nil {
		t.Fatalf("ComputeModelRollups() error = %v", err)
	}
	for _, r := range rollups {
		if r.ModelID == model.ID {
			t.Errorf("rollup for model %q appeared with a zero-width window; rollups: %+v", model.ID, rollups)
		}
	}
}
