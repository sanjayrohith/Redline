package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/bench"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/inference"
)

// DeploymentGetter is the persistence dependency CreateBenchmarkRunHandler
// needs to resolve the deployment a run pins against. It is satisfied by
// *db.DeploymentRepository.
type DeploymentGetter interface {
	GetByID(ctx context.Context, id string) (*db.Deployment, error)
}

// BenchmarkRunStore is the persistence dependency CreateBenchmarkRunHandler
// and ListBenchmarkRunsHandler need. It is satisfied by
// *db.BenchmarkRunRepository.
type BenchmarkRunStore interface {
	Create(ctx context.Context, run db.NewBenchmarkRun) (*db.BenchmarkRun, error)
	ListByModel(ctx context.Context, modelID string) ([]db.BenchmarkRun, error)
}

// BenchmarkRunner executes the standardized task suite. It is satisfied
// by bench.Run.
type BenchmarkRunner func(ctx context.Context, backend inference.Backend, suite []bench.Task, cfg bench.SamplingConfig) (*bench.Result, error)

// BenchmarkRunView is one benchmark run as returned to a client.
type BenchmarkRunView struct {
	ID           string  `json:"id"`
	DeploymentID string  `json:"deployment_id"`
	ModelID      string  `json:"model_id"`
	Precision    string  `json:"precision"`
	Temperature  float64 `json:"temperature"`
	TopP         float64 `json:"top_p"`
	Seed         *int64  `json:"seed,omitempty"`
	TaskCount    int     `json:"task_count"`
	PassedCount  int     `json:"passed_count"`
	Score        float64 `json:"score"`
	CreatedAt    int64   `json:"created_at"`
	// Tasks is populated only on the response to the run that produced it -
	// not on historical listings, which return the aggregate score alone.
	Tasks []bench.TaskResult `json:"tasks,omitempty"`
}

func benchmarkRunView(r *db.BenchmarkRun) BenchmarkRunView {
	return BenchmarkRunView{
		ID: r.ID, DeploymentID: r.DeploymentID, ModelID: r.ModelID, Precision: r.Precision,
		Temperature: r.Temperature, TopP: r.TopP, Seed: r.Seed,
		TaskCount: r.TaskCount, PassedCount: r.PassedCount, Score: r.Score, CreatedAt: r.CreatedAt.Unix(),
	}
}

type createBenchmarkRunRequest struct {
	DeploymentID string  `json:"deployment_id"`
	Precision    string  `json:"precision"`
	Temperature  float64 `json:"temperature"`
	TopP         float64 `json:"top_p"`
	Seed         *int64  `json:"seed,omitempty"`
}

// CreateBenchmarkRunHandler implements POST /v1/dashboard/benchmarks:
// dispatches the standardized evaluation suite against the pinned
// deployment's model at the caller's explicit sampling configuration,
// scores it, persists the result, and returns it with its full per-task
// breakdown.
func CreateBenchmarkRunHandler(deployments DeploymentGetter, runs BenchmarkRunStore, backend inference.Backend, runner BenchmarkRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		var req createBenchmarkRunRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "malformed request body", requestID)
			return
		}
		if req.DeploymentID == "" {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "deployment_id is required", requestID)
			return
		}
		if req.Temperature < 0 || req.Temperature > 2 {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "temperature must be between 0 and 2", requestID)
			return
		}
		if req.TopP <= 0 || req.TopP > 1 {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "top_p must be greater than 0 and at most 1", requestID)
			return
		}
		if req.Precision == "" {
			req.Precision = "fp16"
		}

		deployment, err := deployments.GetByID(r.Context(), req.DeploymentID)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		result, err := runner(r.Context(), backend, bench.DefaultSuite, bench.SamplingConfig{
			Model: deployment.ModelID, Temperature: req.Temperature, TopP: req.TopP, Seed: req.Seed,
		})
		if err != nil {
			apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "benchmark run failed", requestID)
			return
		}

		record, err := runs.Create(r.Context(), db.NewBenchmarkRun{
			DeploymentID: deployment.ID, ModelID: deployment.ModelID, Precision: req.Precision,
			Temperature: req.Temperature, TopP: req.TopP, Seed: req.Seed,
			TaskCount: len(result.Tasks), PassedCount: result.PassedCount, Score: result.Score,
		})
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		view := benchmarkRunView(record)
		view.Tasks = result.Tasks

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(view)
	}
}

// ListBenchmarkRunsHandler implements GET /v1/dashboard/benchmarks?model_id=:
// every historical run for a model, newest first, for comparison across
// precisions and time.
func ListBenchmarkRunsHandler(runs BenchmarkRunStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		modelID := r.URL.Query().Get("model_id")
		if modelID == "" {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "model_id query parameter is required", requestID)
			return
		}

		records, err := runs.ListByModel(r.Context(), modelID)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		views := make([]BenchmarkRunView, len(records))
		for i, r := range records {
			views[i] = benchmarkRunView(&r)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(struct {
			Data []BenchmarkRunView `json:"data"`
		}{Data: views})
	}
}
