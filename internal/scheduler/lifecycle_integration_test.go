package scheduler_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/nomadclient"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

// TestNomadLifecycle_SubmitStatusTransitionIdempotentResubmitStop drives a
// single Nomad dev agent through the full deployment lifecycle this
// gateway relies on: submit, observe the status transition to running,
// resubmit idempotently (no duplicate allocation), then stop and observe
// the allocation's desired status flip to "stop".
func TestNomadLifecycle_SubmitStatusTransitionIdempotentResubmitStop(t *testing.T) {
	addr := startTestAgent(t)

	client, err := nomadclient.NewClient(addr)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	updater := newFakeDeploymentStateUpdater()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	streamer := scheduler.NewStatusStreamer(client, updater, logger)

	streamCtx, cancelStream := context.WithCancel(context.Background())
	defer cancelStream()
	go func() { _ = streamer.Run(streamCtx) }()
	time.Sleep(500 * time.Millisecond) // let the stream connect before we submit

	const deploymentID = "dep-lifecycle-test"
	job := rawExecInferenceJob(deploymentID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Submit.
	evalID, alreadyExisted, err := client.SubmitJobIdempotent(ctx, job)
	if err != nil {
		t.Fatalf("SubmitJobIdempotent() error = %v", err)
	}
	if alreadyExisted {
		t.Fatal("first submission: alreadyExisted = true, want false")
	}
	if evalID == "" {
		t.Fatal("first submission: evalID is empty")
	}

	// 2. Status transition: the streamer should persist "ready" once the
	// allocation starts running.
	deadline := time.Now().Add(20 * time.Second)
	var states []string
	for time.Now().Before(deadline) {
		states = updater.statesFor(deploymentID)
		if len(states) > 0 && states[len(states)-1] == "ready" {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if len(states) == 0 || states[len(states)-1] != "ready" {
		t.Fatalf("deployment state transitions = %v, want the final state to be ready", states)
	}

	var allocID string
	deadline = time.Now().Add(10 * time.Second)
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

	// 3. Idempotent resubmission: simulating a client retry after a
	// network timeout must not create a second allocation.
	evalID2, alreadyExisted2, err := client.SubmitJobIdempotent(ctx, job)
	if err != nil {
		t.Fatalf("second SubmitJobIdempotent() error = %v", err)
	}
	if !alreadyExisted2 {
		t.Error("second submission: alreadyExisted = false, want true")
	}
	if evalID2 != "" {
		t.Errorf("second submission: evalID = %q, want empty", evalID2)
	}

	allocs, err := client.AllocationsForJob(ctx, *job.ID)
	if err != nil {
		t.Fatalf("AllocationsForJob() error = %v", err)
	}
	if len(allocs) != 1 {
		t.Errorf("allocation count after idempotent resubmit = %d, want 1", len(allocs))
	}

	// 4. Stop: the job deregisters and the allocation's desired status
	// flips to stop. purge=false here so the allocation record is still
	// readable for the assertion below; the cleanup does a final purge.
	if _, err := client.StopJob(ctx, *job.ID, false); err != nil {
		t.Fatalf("StopJob() error = %v", err)
	}
	t.Cleanup(func() { _, _ = client.StopJob(context.Background(), *job.ID, true) })

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
