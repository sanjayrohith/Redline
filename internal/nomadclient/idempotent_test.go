package nomadclient

import (
	"context"
	"testing"
	"time"
)

func TestClient_SubmitJobIdempotent_FirstCallRegistersSecondSkips(t *testing.T) {
	addr := startTestAgent(t)

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := trivialJob("nomadclient-idempotent-job")

	evalID1, existed1, err := client.SubmitJobIdempotent(ctx, job)
	if err != nil {
		t.Fatalf("first SubmitJobIdempotent() error = %v", err)
	}
	if existed1 {
		t.Error("first call: alreadyExisted = true, want false")
	}
	if evalID1 == "" {
		t.Error("first call: evalID is empty")
	}

	// Simulate a client retrying after a network timeout: same job, same
	// deterministic ID.
	evalID2, existed2, err := client.SubmitJobIdempotent(ctx, job)
	if err != nil {
		t.Fatalf("second SubmitJobIdempotent() error = %v", err)
	}
	if !existed2 {
		t.Error("second call: alreadyExisted = false, want true (should reconcile onto the existing job)")
	}
	if evalID2 != "" {
		t.Errorf("second call: evalID = %q, want empty (no new evaluation should be created)", evalID2)
	}

	// Confirm only a single allocation was ever produced - no duplicate.
	deadline := time.Now().Add(15 * time.Second)
	var allocCount int
	for time.Now().Before(deadline) {
		allocs, err := client.AllocationsForJob(ctx, *job.ID)
		if err != nil {
			t.Fatalf("AllocationsForJob() error = %v", err)
		}
		allocCount = len(allocs)
		if allocCount > 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if allocCount != 1 {
		t.Errorf("allocation count = %d, want exactly 1", allocCount)
	}

	if _, err := client.StopJob(ctx, *job.ID, true); err != nil {
		t.Fatalf("StopJob() error = %v", err)
	}
}

func TestClient_SubmitJobIdempotent_ResubmitsAfterJobIsDead(t *testing.T) {
	addr := startTestAgent(t)

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	job := trivialJob("nomadclient-idempotent-dead-job")

	if _, existed, err := client.SubmitJobIdempotent(ctx, job); err != nil || existed {
		t.Fatalf("first submit: existed = %v, err = %v", existed, err)
	}

	if _, err := client.StopJob(ctx, *job.ID, true); err != nil {
		t.Fatalf("StopJob() error = %v", err)
	}

	// Purged, so Nomad has no record of the job at all - this must be
	// treated as a fresh submission, not skipped.
	evalID, existed, err := client.SubmitJobIdempotent(ctx, job)
	if err != nil {
		t.Fatalf("SubmitJobIdempotent() after purge error = %v", err)
	}
	if existed {
		t.Error("alreadyExisted = true, want false after the job was purged")
	}
	if evalID == "" {
		t.Error("evalID is empty, want a fresh evaluation after re-registering a purged job")
	}
}

func TestClient_SubmitJobIdempotent_RequiresJobID(t *testing.T) {
	addr := startTestAgent(t)

	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	job := trivialJob("placeholder")
	job.ID = nil

	if _, _, err := client.SubmitJobIdempotent(context.Background(), job); err == nil {
		t.Fatal("SubmitJobIdempotent() error = nil, want error for a job with no ID")
	}
}
