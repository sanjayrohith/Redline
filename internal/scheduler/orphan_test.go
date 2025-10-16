package scheduler

import (
	"context"
	"testing"

	"github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeOrphanOrchestrator struct {
	allocs  []*api.AllocationListStub
	stopped []string
}

func (f *fakeOrphanOrchestrator) ListAllocations(_ context.Context) ([]*api.AllocationListStub, error) {
	return f.allocs, nil
}

func (f *fakeOrphanOrchestrator) StopAllocation(_ context.Context, allocID string) error {
	f.stopped = append(f.stopped, allocID)
	return nil
}

type fakeActiveDeploymentLister struct {
	active []db.Deployment
}

func (f *fakeActiveDeploymentLister) ListActive(_ context.Context) ([]db.Deployment, error) {
	return f.active, nil
}

func TestOrphanReconciler_StopsAllocationWithNoActiveRecord(t *testing.T) {
	orch := &fakeOrphanOrchestrator{allocs: []*api.AllocationListStub{
		{ID: "alloc-orphan", JobID: "inference-dep-orphan", ClientStatus: "running"},
	}}
	deployments := &fakeActiveDeploymentLister{} // no active deployments at all

	reconciler := NewOrphanReconciler(orch, deployments, testLogger())

	stopped, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(stopped) != 1 || stopped[0] != "alloc-orphan" {
		t.Errorf("stopped = %v, want [alloc-orphan]", stopped)
	}
	if len(orch.stopped) != 1 {
		t.Errorf("orch.stopped = %v, want one stop call", orch.stopped)
	}
}

func TestOrphanReconciler_LeavesKnownAllocationAlone(t *testing.T) {
	allocID := "alloc-known"
	orch := &fakeOrphanOrchestrator{allocs: []*api.AllocationListStub{
		{ID: allocID, JobID: "inference-dep-known", ClientStatus: "running"},
	}}
	deployments := &fakeActiveDeploymentLister{active: []db.Deployment{
		{ID: "dep-known", AllocationID: &allocID, State: string(StateReady)},
	}}

	reconciler := NewOrphanReconciler(orch, deployments, testLogger())

	stopped, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(stopped) != 0 {
		t.Errorf("stopped = %v, want none", stopped)
	}
}

func TestOrphanReconciler_IgnoresNonInferenceJobs(t *testing.T) {
	orch := &fakeOrphanOrchestrator{allocs: []*api.AllocationListStub{
		{ID: "alloc-other", JobID: "some-other-job", ClientStatus: "running"},
	}}
	deployments := &fakeActiveDeploymentLister{}

	reconciler := NewOrphanReconciler(orch, deployments, testLogger())

	stopped, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(stopped) != 0 {
		t.Errorf("stopped = %v, want none (not an inference job)", stopped)
	}
}

func TestOrphanReconciler_IgnoresDeadAllocations(t *testing.T) {
	orch := &fakeOrphanOrchestrator{allocs: []*api.AllocationListStub{
		{ID: "alloc-dead", JobID: "inference-dep-dead", ClientStatus: "complete"},
		{ID: "alloc-failed", JobID: "inference-dep-failed", ClientStatus: "failed"},
	}}
	deployments := &fakeActiveDeploymentLister{}

	reconciler := NewOrphanReconciler(orch, deployments, testLogger())

	stopped, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(stopped) != 0 {
		t.Errorf("stopped = %v, want none (already dead, nothing to reclaim)", stopped)
	}
}

func TestOrphanReconciler_MixedFleet(t *testing.T) {
	knownAllocID := "alloc-known"
	orch := &fakeOrphanOrchestrator{allocs: []*api.AllocationListStub{
		{ID: knownAllocID, JobID: "inference-dep-known", ClientStatus: "running"},
		{ID: "alloc-orphan-1", JobID: "inference-dep-orphan-1", ClientStatus: "pending"},
		{ID: "alloc-orphan-2", JobID: "inference-dep-orphan-2", ClientStatus: "running"},
		{ID: "alloc-dead", JobID: "inference-dep-dead", ClientStatus: "complete"},
		{ID: "alloc-unrelated", JobID: "unrelated-job", ClientStatus: "running"},
	}}
	deployments := &fakeActiveDeploymentLister{active: []db.Deployment{
		{ID: "dep-known", AllocationID: &knownAllocID, State: string(StateReady)},
	}}

	reconciler := NewOrphanReconciler(orch, deployments, testLogger())

	stopped, err := reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if len(stopped) != 2 {
		t.Fatalf("len(stopped) = %d, want 2", len(stopped))
	}
	stoppedSet := map[string]bool{stopped[0]: true, stopped[1]: true}
	if !stoppedSet["alloc-orphan-1"] || !stoppedSet["alloc-orphan-2"] {
		t.Errorf("stopped = %v, want exactly the two orphans", stopped)
	}
}
