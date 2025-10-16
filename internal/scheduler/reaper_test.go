package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeReadyLister struct {
	deployments []db.Deployment
}

func (f *fakeReadyLister) ListByState(_ context.Context, _ string) ([]db.Deployment, error) {
	return f.deployments, nil
}

type fakeIdleReader struct {
	durations map[string]time.Duration
}

func (f *fakeIdleReader) IdleDuration(_ context.Context, allocationID string) (time.Duration, error) {
	d, ok := f.durations[allocationID]
	if !ok {
		return 0, ErrNoIdleRecord
	}
	return d, nil
}

type fakeAllocationStopper struct {
	stopped []string
}

func (f *fakeAllocationStopper) StopAllocation(_ context.Context, allocID string) error {
	f.stopped = append(f.stopped, allocID)
	return nil
}

type fakeStateStore struct {
	updates map[string]DeploymentState
}

func newFakeStateStore() *fakeStateStore {
	return &fakeStateStore{updates: map[string]DeploymentState{}}
}

func (f *fakeStateStore) CurrentState(_ context.Context, id string) (DeploymentState, error) {
	if s, ok := f.updates[id]; ok {
		return s, nil
	}
	return StateReady, nil
}

func (f *fakeStateStore) UpdateState(_ context.Context, id string, state DeploymentState) error {
	f.updates[id] = state
	return nil
}

func (f *fakeStateStore) SetAllocationID(_ context.Context, _, _ string) error { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func allocIDPtr(s string) *string { return &s }

func TestIdleReaper_ReapsAllocationPastThreshold(t *testing.T) {
	deployedAt := time.Now().Add(-2 * time.Hour)
	lister := &fakeReadyLister{deployments: []db.Deployment{
		{ID: "dep-1", AllocationID: allocIDPtr("alloc-1"), State: string(StateReady), CreatedAt: deployedAt},
	}}
	idle := &fakeIdleReader{durations: map[string]time.Duration{"alloc-1": 400 * time.Second}}
	stopper := &fakeAllocationStopper{}
	states := newFakeStateStore()

	reaper := NewIdleReaper(lister, idle, stopper, states, 300*time.Second, testLogger())

	results, err := reaper.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].DeploymentID != "dep-1" || results[0].AllocationID != "alloc-1" {
		t.Errorf("result = %+v", results[0])
	}
	if results[0].GPUHours < 1.9 || results[0].GPUHours > 2.1 {
		t.Errorf("GPUHours = %v, want ~2.0", results[0].GPUHours)
	}

	if len(stopper.stopped) != 1 || stopper.stopped[0] != "alloc-1" {
		t.Errorf("stopped = %v, want [alloc-1]", stopper.stopped)
	}
	if states.updates["dep-1"] != StateDraining {
		t.Errorf("state = %q, want draining", states.updates["dep-1"])
	}
}

func TestIdleReaper_SkipsAllocationUnderThreshold(t *testing.T) {
	lister := &fakeReadyLister{deployments: []db.Deployment{
		{ID: "dep-1", AllocationID: allocIDPtr("alloc-1"), State: string(StateReady), CreatedAt: time.Now()},
	}}
	idle := &fakeIdleReader{durations: map[string]time.Duration{"alloc-1": 60 * time.Second}}
	stopper := &fakeAllocationStopper{}
	states := newFakeStateStore()

	reaper := NewIdleReaper(lister, idle, stopper, states, 300*time.Second, testLogger())

	results, err := reaper.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
	if len(stopper.stopped) != 0 {
		t.Errorf("stopped = %v, want none", stopper.stopped)
	}
}

func TestIdleReaper_SkipsDeploymentWithoutAllocationID(t *testing.T) {
	lister := &fakeReadyLister{deployments: []db.Deployment{
		{ID: "dep-1", AllocationID: nil, State: string(StateReady), CreatedAt: time.Now()},
	}}
	idle := &fakeIdleReader{durations: map[string]time.Duration{}}
	stopper := &fakeAllocationStopper{}
	states := newFakeStateStore()

	reaper := NewIdleReaper(lister, idle, stopper, states, 300*time.Second, testLogger())

	results, err := reaper.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}

func TestIdleReaper_SkipsDeploymentWithNoIdleRecord(t *testing.T) {
	lister := &fakeReadyLister{deployments: []db.Deployment{
		{ID: "dep-1", AllocationID: allocIDPtr("alloc-1"), State: string(StateReady), CreatedAt: time.Now()},
	}}
	idle := &fakeIdleReader{durations: map[string]time.Duration{}} // no record for alloc-1
	stopper := &fakeAllocationStopper{}
	states := newFakeStateStore()

	reaper := NewIdleReaper(lister, idle, stopper, states, 300*time.Second, testLogger())

	if _, err := reaper.Reap(context.Background()); err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if len(stopper.stopped) != 0 {
		t.Error("should not stop an allocation with no idle record")
	}
}

func TestIdleReaper_DefaultThreshold(t *testing.T) {
	reaper := NewIdleReaper(&fakeReadyLister{}, &fakeIdleReader{}, &fakeAllocationStopper{}, newFakeStateStore(), 0, testLogger())
	if reaper.Threshold != DefaultIdleThreshold {
		t.Errorf("Threshold = %v, want %v", reaper.Threshold, DefaultIdleThreshold)
	}
}

func TestIdleReaper_MultipleDeploymentsMixedOutcomes(t *testing.T) {
	lister := &fakeReadyLister{deployments: []db.Deployment{
		{ID: "dep-idle", AllocationID: allocIDPtr("alloc-idle"), State: string(StateReady), CreatedAt: time.Now().Add(-time.Hour)},
		{ID: "dep-active", AllocationID: allocIDPtr("alloc-active"), State: string(StateReady), CreatedAt: time.Now()},
	}}
	idle := &fakeIdleReader{durations: map[string]time.Duration{
		"alloc-idle":   400 * time.Second,
		"alloc-active": 10 * time.Second,
	}}
	stopper := &fakeAllocationStopper{}
	states := newFakeStateStore()

	reaper := NewIdleReaper(lister, idle, stopper, states, 300*time.Second, testLogger())

	results, err := reaper.Reap(context.Background())
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if len(results) != 1 || results[0].DeploymentID != "dep-idle" {
		t.Errorf("results = %+v, want exactly dep-idle reaped", results)
	}
}
