package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	nomadAPI "github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeDeploymentCreator struct {
	created *db.Deployment
	nextID  string
}

func (f *fakeDeploymentCreator) Create(_ context.Context, modelID, _ string) (*db.Deployment, error) {
	d := &db.Deployment{ID: f.nextID, ModelID: modelID, State: "queued"}
	f.created = d
	return d, nil
}

type fakeJobDispatcher struct {
	queued     bool
	degraded   bool
	dispatched []string
}

func (f *fakeJobDispatcher) Dispatch(_ context.Context, deploymentID string, _ *nomadAPI.Job) (string, bool, error) {
	f.dispatched = append(f.dispatched, deploymentID)
	return "eval-1", f.queued, nil
}

func (f *fakeJobDispatcher) Degraded() bool { return f.degraded }

func testBuildJob(string, *db.Model) *nomadAPI.Job { return &nomadAPI.Job{} }

func TestCreateDeploymentHandler_DispatchesDirectlyWhenHealthy(t *testing.T) {
	models := &fakeModelGetter{byID: map[string]*db.Model{"model-1": {ID: "model-1"}}}
	deployments := &fakeDeploymentCreator{nextID: "dep-1"}
	dispatcher := &fakeJobDispatcher{}

	handler := CreateDeploymentHandler(deployments, models, dispatcher, testBuildJob)

	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/deployments", strings.NewReader(`{"model_id":"model-1"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
	var view DeploymentView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.Queued || view.Degraded {
		t.Errorf("view = %+v, want queued=false degraded=false", view)
	}
	if len(dispatcher.dispatched) != 1 || dispatcher.dispatched[0] != "dep-1" {
		t.Errorf("dispatched = %v, want [dep-1]", dispatcher.dispatched)
	}
}

func TestCreateDeploymentHandler_ReportsQueuedAndDegradedWhenNomadIsDown(t *testing.T) {
	models := &fakeModelGetter{byID: map[string]*db.Model{"model-1": {ID: "model-1"}}}
	deployments := &fakeDeploymentCreator{nextID: "dep-1"}
	dispatcher := &fakeJobDispatcher{queued: true, degraded: true}

	handler := CreateDeploymentHandler(deployments, models, dispatcher, testBuildJob)

	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/deployments", strings.NewReader(`{"model_id":"model-1"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 - a Nomad outage must not fail the request, body = %s", rec.Code, rec.Body.String())
	}
	var view DeploymentView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !view.Queued || !view.Degraded {
		t.Errorf("view = %+v, want queued=true degraded=true", view)
	}
}

func TestCreateDeploymentHandler_RejectsUnknownModel(t *testing.T) {
	handler := CreateDeploymentHandler(&fakeDeploymentCreator{}, &fakeModelGetter{byID: map[string]*db.Model{}}, &fakeJobDispatcher{}, testBuildJob)

	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/deployments", strings.NewReader(`{"model_id":"nope"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestSchedulerStatusHandler_ReportsDegradedState(t *testing.T) {
	handler := SchedulerStatusHandler(&fakeJobDispatcher{degraded: true})

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/scheduler/status", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var view SchedulerStatusView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !view.Degraded {
		t.Error("Degraded = false, want true")
	}
}
