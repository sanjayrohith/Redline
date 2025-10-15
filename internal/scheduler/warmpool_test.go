package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeWarmPoolFinder struct {
	deployments []db.Deployment
	err         error
}

func (f *fakeWarmPoolFinder) ListByModel(_ context.Context, _ string) ([]db.Deployment, error) {
	return f.deployments, f.err
}

func TestFindWarmDeployment_ReturnsReadyDeployment(t *testing.T) {
	finder := &fakeWarmPoolFinder{deployments: []db.Deployment{
		{ID: "dep-1", State: string(StateDraining)},
		{ID: "dep-2", State: string(StateReady)},
		{ID: "dep-3", State: string(StateProvisioning)},
	}}

	got, err := FindWarmDeployment(context.Background(), finder, "model-1")
	if err != nil {
		t.Fatalf("FindWarmDeployment() error = %v", err)
	}
	if got == nil || got.ID != "dep-2" {
		t.Fatalf("got = %+v, want dep-2", got)
	}
}

func TestFindWarmDeployment_NoneReadyReturnsNilNoError(t *testing.T) {
	finder := &fakeWarmPoolFinder{deployments: []db.Deployment{
		{ID: "dep-1", State: string(StateProvisioning)},
		{ID: "dep-2", State: string(StateFailed)},
	}}

	got, err := FindWarmDeployment(context.Background(), finder, "model-1")
	if err != nil {
		t.Fatalf("FindWarmDeployment() error = %v", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

func TestFindWarmDeployment_EmptyList(t *testing.T) {
	finder := &fakeWarmPoolFinder{}

	got, err := FindWarmDeployment(context.Background(), finder, "model-1")
	if err != nil {
		t.Fatalf("FindWarmDeployment() error = %v", err)
	}
	if got != nil {
		t.Errorf("got = %+v, want nil", got)
	}
}

func TestFindWarmDeployment_PropagatesError(t *testing.T) {
	finder := &fakeWarmPoolFinder{err: errors.New("connection reset")}

	if _, err := FindWarmDeployment(context.Background(), finder, "model-1"); err == nil {
		t.Fatal("FindWarmDeployment() error = nil, want propagated error")
	}
}

func TestFindWarmDeployment_FirstReadyWinsWhenMultiple(t *testing.T) {
	finder := &fakeWarmPoolFinder{deployments: []db.Deployment{
		{ID: "newest-ready", State: string(StateReady)},
		{ID: "older-ready", State: string(StateReady)},
	}}

	// ListByModel orders newest first; the warm pool should route to
	// whichever ready deployment it encounters first in that order.
	got, err := FindWarmDeployment(context.Background(), finder, "model-1")
	if err != nil {
		t.Fatalf("FindWarmDeployment() error = %v", err)
	}
	if got.ID != "newest-ready" {
		t.Errorf("got.ID = %q, want newest-ready", got.ID)
	}
}
