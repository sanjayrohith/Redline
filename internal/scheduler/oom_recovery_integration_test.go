package scheduler_test

import (
	"context"
	"testing"

	"github.com/sanjayrohith/redline/internal/scheduler"
)

func TestNodeVRAMCeiling_CeilingForUnknownNodeReportsNoRecord(t *testing.T) {
	client := newTestRedisClient(t)
	ceiling := scheduler.NewNodeVRAMCeiling(client)
	ctx := context.Background()

	_, ok, err := ceiling.CeilingFor(ctx, "node-1")
	if err != nil {
		t.Fatalf("CeilingFor() error = %v", err)
	}
	if ok {
		t.Error("CeilingFor() ok = true for a node with no recorded downgrade, want false")
	}
}

func TestNodeVRAMCeiling_DowngradeRecordsTheObservedCeiling(t *testing.T) {
	client := newTestRedisClient(t)
	ceiling := scheduler.NewNodeVRAMCeiling(client)
	ctx := context.Background()

	if err := ceiling.Downgrade(ctx, "node-1", 40<<30); err != nil {
		t.Fatalf("Downgrade() error = %v", err)
	}

	bytes, ok, err := ceiling.CeilingFor(ctx, "node-1")
	if err != nil {
		t.Fatalf("CeilingFor() error = %v", err)
	}
	if !ok || bytes != 40<<30 {
		t.Errorf("CeilingFor() = (%d, %v), want (%d, true)", bytes, ok, int64(40<<30))
	}
}

func TestNodeVRAMCeiling_DowngradeIsRatchetedOneWay(t *testing.T) {
	client := newTestRedisClient(t)
	ceiling := scheduler.NewNodeVRAMCeiling(client)
	ctx := context.Background()

	if err := ceiling.Downgrade(ctx, "node-1", 40<<30); err != nil {
		t.Fatalf("first Downgrade() error = %v", err)
	}

	// A higher observation must not raise the ceiling back up: a prior,
	// harder OOM is stronger evidence than a later success.
	if err := ceiling.Downgrade(ctx, "node-1", 60<<30); err != nil {
		t.Fatalf("higher Downgrade() error = %v", err)
	}
	bytes, _, err := ceiling.CeilingFor(ctx, "node-1")
	if err != nil {
		t.Fatalf("CeilingFor() error = %v", err)
	}
	if bytes != 40<<30 {
		t.Errorf("CeilingFor() = %d after a higher observation, want it to stay at %d", bytes, int64(40<<30))
	}

	// A lower observation must ratchet it down further.
	if err := ceiling.Downgrade(ctx, "node-1", 32<<30); err != nil {
		t.Fatalf("lower Downgrade() error = %v", err)
	}
	bytes, _, err = ceiling.CeilingFor(ctx, "node-1")
	if err != nil {
		t.Fatalf("CeilingFor() error = %v", err)
	}
	if bytes != 32<<30 {
		t.Errorf("CeilingFor() = %d after a lower observation, want %d", bytes, int64(32<<30))
	}
}

func TestNodeVRAMCeiling_DowngradesAreIndependentPerNode(t *testing.T) {
	client := newTestRedisClient(t)
	ceiling := scheduler.NewNodeVRAMCeiling(client)
	ctx := context.Background()

	if err := ceiling.Downgrade(ctx, "node-1", 40<<30); err != nil {
		t.Fatalf("Downgrade(node-1) error = %v", err)
	}

	_, ok, err := ceiling.CeilingFor(ctx, "node-2")
	if err != nil {
		t.Fatalf("CeilingFor(node-2) error = %v", err)
	}
	if ok {
		t.Error("CeilingFor(node-2) ok = true, want a downgrade on node-1 to not affect node-2")
	}
}
