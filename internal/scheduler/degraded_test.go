package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/scheduler"
)

type fakeJobSubmitter struct {
	evalID string
	err    error
	calls  int
}

func (f *fakeJobSubmitter) SubmitJobIdempotent(context.Context, *api.Job) (string, bool, error) {
	f.calls++
	if f.err != nil {
		return "", false, f.err
	}
	return f.evalID, false, nil
}

type fakeDeploymentQueue struct {
	enqueued []string
	err      error
}

func (f *fakeDeploymentQueue) Enqueue(_ context.Context, id, _ string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, id)
	return nil
}

func TestDegradedModeDispatcher_DispatchesDirectlyWhenNomadIsReachable(t *testing.T) {
	orchestrator := &fakeJobSubmitter{evalID: "eval-1"}
	queue := &fakeDeploymentQueue{}
	d := scheduler.NewDegradedModeDispatcher(orchestrator, queue)

	evalID, queued, err := d.Dispatch(context.Background(), "dep-1", &api.Job{})
	if err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if queued {
		t.Error("queued = true, want false when Nomad accepts the job")
	}
	if evalID != "eval-1" {
		t.Errorf("evalID = %q, want eval-1", evalID)
	}
	if d.Degraded() {
		t.Error("Degraded() = true, want false after a successful dispatch")
	}
	if len(queue.enqueued) != 0 {
		t.Errorf("enqueued = %v, want none", queue.enqueued)
	}
}

func TestDegradedModeDispatcher_QueuesInsteadOfFailingWhenNomadIsUnreachable(t *testing.T) {
	orchestrator := &fakeJobSubmitter{err: errors.New("dial tcp: connection refused")}
	queue := &fakeDeploymentQueue{}
	d := scheduler.NewDegradedModeDispatcher(orchestrator, queue)

	evalID, queued, err := d.Dispatch(context.Background(), "dep-1", &api.Job{})
	if err != nil {
		t.Fatalf("Dispatch() error = %v, want nil - a Nomad outage must not fail the caller's request", err)
	}
	if !queued {
		t.Error("queued = false, want true when Nomad is unreachable")
	}
	if evalID != "" {
		t.Errorf("evalID = %q, want empty when queued", evalID)
	}
	if !d.Degraded() {
		t.Error("Degraded() = false, want true after a failed dispatch")
	}
	if len(queue.enqueued) != 1 || queue.enqueued[0] != "dep-1" {
		t.Errorf("enqueued = %v, want [dep-1]", queue.enqueued)
	}
}

func TestDegradedModeDispatcher_RecoversAfterNomadComesBack(t *testing.T) {
	orchestrator := &fakeJobSubmitter{err: errors.New("unreachable")}
	d := scheduler.NewDegradedModeDispatcher(orchestrator, &fakeDeploymentQueue{})

	if _, _, err := d.Dispatch(context.Background(), "dep-1", &api.Job{}); err != nil {
		t.Fatalf("Dispatch() error = %v", err)
	}
	if !d.Degraded() {
		t.Fatal("Degraded() = false, want true after the failed dispatch")
	}

	orchestrator.err = nil
	orchestrator.evalID = "eval-recovered"
	if _, queued, err := d.Dispatch(context.Background(), "dep-2", &api.Job{}); err != nil || queued {
		t.Fatalf("Dispatch() = (_, %v, %v), want (_, false, nil) once Nomad recovers", queued, err)
	}
	if d.Degraded() {
		t.Error("Degraded() = true, want false after a subsequent successful dispatch")
	}
}

func TestDegradedModeDispatcher_ReturnsErrorWhenBothNomadAndQueueFail(t *testing.T) {
	orchestrator := &fakeJobSubmitter{err: errors.New("nomad unreachable")}
	queue := &fakeDeploymentQueue{err: errors.New("redis unreachable")}
	d := scheduler.NewDegradedModeDispatcher(orchestrator, queue)

	_, _, err := d.Dispatch(context.Background(), "dep-1", &api.Job{})
	if err == nil {
		t.Fatal("Dispatch() error = nil, want an error when both Nomad and the retry queue fail")
	}
}
