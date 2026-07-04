package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

// DeploymentCreator is the persistence dependency
// CreateDeploymentHandler needs. It is satisfied by
// *db.DeploymentRepository.
type DeploymentCreator interface {
	Create(ctx context.Context, modelID string) (*db.Deployment, error)
}

// JobDispatcher submits a deployment's Nomad job, degrading to a durable
// queue rather than failing when Nomad is unreachable. It is satisfied by
// *scheduler.DegradedModeDispatcher.
type JobDispatcher interface {
	Dispatch(ctx context.Context, deploymentID string, job *api.Job) (evalID string, queued bool, err error)
	Degraded() bool
}

// JobSpecBuilder builds the Nomad job for a deployment. It is satisfied
// by a closure over nomadclient.BuildInferenceJob supplying this
// gateway's fixed defaults (image, resource shape) for the deployment id
// and model this handler resolves.
type JobSpecBuilder func(deploymentID string, model *db.Model) *api.Job

// DeploymentView is one deployment as returned to a client.
type DeploymentView struct {
	ID       string `json:"id"`
	ModelID  string `json:"model_id"`
	State    string `json:"state"`
	Queued   bool   `json:"queued"`
	Degraded bool   `json:"degraded"`
}

type createDeploymentRequest struct {
	ModelID string `json:"model_id"`
}

// CreateDeploymentHandler implements POST /v1/dashboard/deployments:
// registers a new deployment for the given model and dispatches its
// Nomad job. If Nomad is unreachable, the deployment is still accepted
// and durably queued for retry rather than the request failing - a
// scheduler outage degrades new-deployment latency, it does not turn
// into a 5xx for the caller.
func CreateDeploymentHandler(deployments DeploymentCreator, models ModelGetter, dispatcher JobDispatcher, buildJob JobSpecBuilder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		var req createDeploymentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ModelID == "" {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "model_id is required", requestID)
			return
		}

		model, err := models.GetByID(r.Context(), req.ModelID)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		deployment, err := deployments.Create(r.Context(), req.ModelID)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		_, queued, err := dispatcher.Dispatch(r.Context(), deployment.ID, buildJob(deployment.ID, model))
		if err != nil {
			apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "failed to dispatch deployment", requestID)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(DeploymentView{
			ID: deployment.ID, ModelID: deployment.ModelID, State: deployment.State,
			Queued: queued, Degraded: dispatcher.Degraded(),
		})
	}
}

// SchedulerStatusView reports whether the scheduler is currently degraded.
type SchedulerStatusView struct {
	Degraded bool `json:"degraded"`
}

// SchedulerStatusHandler implements GET /v1/dashboard/scheduler/status:
// whether the most recent deployment dispatch had to fall back to
// queuing because Nomad was unreachable.
func SchedulerStatusHandler(dispatcher JobDispatcher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SchedulerStatusView{Degraded: dispatcher.Degraded()})
	}
}
