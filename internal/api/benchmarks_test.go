package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/bench"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/inference"
)

type fakeDeploymentGetter struct {
	byID map[string]*db.Deployment
}

func (f *fakeDeploymentGetter) GetByID(_ context.Context, id string) (*db.Deployment, error) {
	d, ok := f.byID[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return d, nil
}

type fakeBenchmarkRunStore struct {
	created []db.NewBenchmarkRun
	byModel map[string][]db.BenchmarkRun
	nextID  string
}

func (f *fakeBenchmarkRunStore) Create(_ context.Context, run db.NewBenchmarkRun) (*db.BenchmarkRun, error) {
	f.created = append(f.created, run)
	return &db.BenchmarkRun{
		ID: f.nextID, DeploymentID: run.DeploymentID, ModelID: run.ModelID, Precision: run.Precision,
		Temperature: run.Temperature, TopP: run.TopP, Seed: run.Seed,
		TaskCount: run.TaskCount, PassedCount: run.PassedCount, Score: run.Score, CreatedAt: time.Now(),
	}, nil
}

func (f *fakeBenchmarkRunStore) ListByModel(_ context.Context, modelID string) ([]db.BenchmarkRun, error) {
	return f.byModel[modelID], nil
}

func fakeRunner(passed, total int) BenchmarkRunner {
	return func(context.Context, inference.Backend, []bench.Task, bench.SamplingConfig) (*bench.Result, error) {
		tasks := make([]bench.TaskResult, total)
		for i := 0; i < total; i++ {
			tasks[i] = bench.TaskResult{Passed: i < passed}
		}
		return &bench.Result{Tasks: tasks, PassedCount: passed, Score: float64(passed) / float64(total)}, nil
	}
}

func TestCreateBenchmarkRunHandler_DispatchesAndPersists(t *testing.T) {
	deployments := &fakeDeploymentGetter{byID: map[string]*db.Deployment{
		"dep-1": {ID: "dep-1", ModelID: "model-1"},
	}}
	runs := &fakeBenchmarkRunStore{nextID: "run-1"}
	handler := CreateBenchmarkRunHandler(deployments, runs, nil, fakeRunner(3, 5))

	body := `{"deployment_id":"dep-1","precision":"fp8","temperature":0.7,"top_p":0.9}`
	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/benchmarks", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
	var view BenchmarkRunView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.Score != 0.6 || view.PassedCount != 3 || view.TaskCount != 5 {
		t.Errorf("view = %+v, want score=0.6 passed=3 count=5", view)
	}
	if len(view.Tasks) != 5 {
		t.Errorf("len(Tasks) = %d, want 5 (per-task breakdown on the creating response)", len(view.Tasks))
	}
	if len(runs.created) != 1 || runs.created[0].ModelID != "model-1" {
		t.Errorf("created = %+v, want one run for model-1 (resolved from the pinned deployment)", runs.created)
	}
}

func TestCreateBenchmarkRunHandler_RejectsUnknownDeployment(t *testing.T) {
	handler := CreateBenchmarkRunHandler(&fakeDeploymentGetter{byID: map[string]*db.Deployment{}}, &fakeBenchmarkRunStore{}, nil, fakeRunner(0, 1))

	body := `{"deployment_id":"nope","temperature":0.5,"top_p":0.9}`
	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/benchmarks", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestCreateBenchmarkRunHandler_RejectsInvalidTemperature(t *testing.T) {
	handler := CreateBenchmarkRunHandler(&fakeDeploymentGetter{}, &fakeBenchmarkRunStore{}, nil, fakeRunner(0, 1))

	body := `{"deployment_id":"dep-1","temperature":5,"top_p":0.9}`
	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/benchmarks", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestListBenchmarkRunsHandler_ReturnsHistoryForModel(t *testing.T) {
	runs := &fakeBenchmarkRunStore{byModel: map[string][]db.BenchmarkRun{
		"model-1": {
			{ID: "run-1", ModelID: "model-1", Precision: "fp16", Score: 0.8, CreatedAt: time.Now()},
			{ID: "run-2", ModelID: "model-1", Precision: "fp8", Score: 0.6, CreatedAt: time.Now()},
		},
	}}
	handler := ListBenchmarkRunsHandler(runs)

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/benchmarks?model_id=model-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body struct {
		Data []BenchmarkRunView `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(body.Data) != 2 {
		t.Fatalf("len(Data) = %d, want 2", len(body.Data))
	}
}

func TestListBenchmarkRunsHandler_RequiresModelID(t *testing.T) {
	handler := ListBenchmarkRunsHandler(&fakeBenchmarkRunStore{})
	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/benchmarks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
