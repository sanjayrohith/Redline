package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/db/migrations"
)

func TestTelemetryRetentionJob_DownsamplesAndPrunesOldSamples(t *testing.T) {
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

	user, err := repos.Users.Create(ctx, "retention@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	model, err := repos.Models.Create(ctx, db.NewModel{
		RepoURL: "org/retention-model", Revision: "main", Architecture: "llama",
		ParameterCount: 1, Dtype: "fp16",
		VRAMEstimateFP16Bytes: 1, VRAMEstimateFP8Bytes: 1, VRAMEstimateInt4Bytes: 1,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	deployment, err := repos.Deployments.Create(ctx, model.ID, "")
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	run, err := repos.InferenceRuns.Create(ctx, deployment.ID, model.ID, user.ID, "alloc-1")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Two raw samples, both already old enough to be past any retention
	// threshold we will pass (retentionThreshold=0 in this test).
	for _, ttft := range []float64{10, 30} {
		v := ttft
		vram := int64(5 << 30)
		if _, err := repos.TelemetrySamples.Create(ctx, db.NewTelemetrySample{
			InferenceRunID: run.ID, TTFTMs: &v, VRAMBytes: &vram,
		}); err != nil {
			t.Fatalf("create sample: %v", err)
		}
	}

	before, err := repos.TelemetrySamples.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun() error = %v", err)
	}
	if len(before) != 2 {
		t.Fatalf("len(before) = %d, want 2", len(before))
	}

	pruned, err := repos.TelemetryRetention.Run(ctx, 0)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if pruned != 2 {
		t.Errorf("pruned = %d, want 2", pruned)
	}

	after, err := repos.TelemetrySamples.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun() error = %v", err)
	}
	if len(after) != 0 {
		t.Errorf("len(after) = %d, want 0 raw samples remaining", len(after))
	}

	aggregates, err := repos.TelemetryRetention.ListHourlyAggregates(ctx, model.ID)
	if err != nil {
		t.Fatalf("ListHourlyAggregates() error = %v", err)
	}
	if len(aggregates) != 1 {
		t.Fatalf("len(aggregates) = %d, want 1", len(aggregates))
	}

	agg := aggregates[0]
	if agg.TTFTCount != 2 {
		t.Errorf("TTFTCount = %d, want 2", agg.TTFTCount)
	}
	if avg := agg.AvgTTFTMs(); avg == nil || *avg != 20 {
		t.Errorf("AvgTTFTMs() = %v, want 20 (mean of 10 and 30)", avg)
	}
	if agg.VRAMMaxBytes == nil || *agg.VRAMMaxBytes != 5<<30 {
		t.Errorf("VRAMMaxBytes = %v, want %v", agg.VRAMMaxBytes, int64(5<<30))
	}
}

func TestTelemetryRetentionJob_LeavesRecentSamplesUntouched(t *testing.T) {
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

	user, err := repos.Users.Create(ctx, "recent@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	model, err := repos.Models.Create(ctx, db.NewModel{
		RepoURL: "org/recent-model", Revision: "main", Architecture: "llama",
		ParameterCount: 1, Dtype: "fp16",
		VRAMEstimateFP16Bytes: 1, VRAMEstimateFP8Bytes: 1, VRAMEstimateInt4Bytes: 1,
	})
	if err != nil {
		t.Fatalf("create model: %v", err)
	}
	deployment, err := repos.Deployments.Create(ctx, model.ID, "")
	if err != nil {
		t.Fatalf("create deployment: %v", err)
	}
	run, err := repos.InferenceRuns.Create(ctx, deployment.ID, model.ID, user.ID, "alloc-1")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	ttft := 15.0
	if _, err := repos.TelemetrySamples.Create(ctx, db.NewTelemetrySample{
		InferenceRunID: run.ID, TTFTMs: &ttft,
	}); err != nil {
		t.Fatalf("create sample: %v", err)
	}

	// A generous retention window (1 hour) must leave a sample created
	// moments ago untouched.
	pruned, err := repos.TelemetryRetention.Run(ctx, time.Hour)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0 with a 1h retention window and a fresh sample", pruned)
	}

	remaining, err := repos.TelemetrySamples.ListByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListByRun() error = %v", err)
	}
	if len(remaining) != 1 {
		t.Errorf("len(remaining) = %d, want 1", len(remaining))
	}
}
