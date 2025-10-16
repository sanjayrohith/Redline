package scheduler_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/nomadclient"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

type staticActiveDeploymentLister struct {
	active []db.Deployment
}

func (s *staticActiveDeploymentLister) ListActive(_ context.Context) ([]db.Deployment, error) {
	return s.active, nil
}

// TestOrphanReconciler_ReapsRealOrphanedAllocation submits a job directly
// through Nomad - bypassing the gateway entirely, simulating a deployment
// whose database record never got written (or was lost) - and asserts
// Reconcile finds and stops it since no active deployment record backs it.
func TestOrphanReconciler_ReapsRealOrphanedAllocation(t *testing.T) {
	addr := startTestAgent(t)

	client, err := nomadclient.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := rawExecInferenceJob("dep-orphan-real")
	if _, err := client.SubmitJob(ctx, job); err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	t.Cleanup(func() { _, _ = client.StopJob(context.Background(), *job.ID, true) })

	var allocID string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		allocs, err := client.AllocationsForJob(ctx, *job.ID)
		if err != nil {
			t.Fatalf("AllocationsForJob() error = %v", err)
		}
		if len(allocs) > 0 {
			allocID = allocs[0].ID
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if allocID == "" {
		t.Fatal("job never produced an allocation")
	}

	// No active deployment record at all: this allocation is a pure orphan.
	reconciler := scheduler.NewOrphanReconciler(client, &staticActiveDeploymentLister{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	stopped, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	found := false
	for _, id := range stopped {
		if id == allocID {
			found = true
		}
	}
	if !found {
		t.Fatalf("stopped = %v, want it to include %s", stopped, allocID)
	}

	deadline = time.Now().Add(10 * time.Second)
	var desiredStatus string
	for time.Now().Before(deadline) {
		alloc, err := client.Allocation(ctx, allocID)
		if err != nil {
			t.Fatalf("Allocation() error = %v", err)
		}
		desiredStatus = alloc.DesiredStatus
		if desiredStatus == "stop" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if desiredStatus != "stop" {
		t.Errorf("DesiredStatus = %q, want stop", desiredStatus)
	}
}

func TestOrphanReconciler_LeavesKnownAllocationRunning(t *testing.T) {
	addr := startTestAgent(t)

	client, err := nomadclient.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := rawExecInferenceJob("dep-known-real")
	if _, err := client.SubmitJob(ctx, job); err != nil {
		t.Fatalf("SubmitJob() error = %v", err)
	}
	t.Cleanup(func() { _, _ = client.StopJob(context.Background(), *job.ID, true) })

	var allocID string
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		allocs, err := client.AllocationsForJob(ctx, *job.ID)
		if err != nil {
			t.Fatalf("AllocationsForJob() error = %v", err)
		}
		if len(allocs) > 0 {
			allocID = allocs[0].ID
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if allocID == "" {
		t.Fatal("job never produced an allocation")
	}

	lister := &staticActiveDeploymentLister{active: []db.Deployment{
		{ID: "dep-known-real", AllocationID: &allocID, State: string(scheduler.StateReady)},
	}}
	reconciler := scheduler.NewOrphanReconciler(client, lister, slog.New(slog.NewTextHandler(io.Discard, nil)))

	stopped, err := reconciler.Reconcile(ctx)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	for _, id := range stopped {
		if id == allocID {
			t.Fatalf("Reconcile() stopped %s, want it left alone (backed by an active deployment record)", allocID)
		}
	}

	alloc, err := client.Allocation(ctx, allocID)
	if err != nil {
		t.Fatalf("Allocation() error = %v", err)
	}
	if alloc.DesiredStatus == "stop" {
		t.Error("DesiredStatus = stop, want the known allocation to remain untouched")
	}
}
