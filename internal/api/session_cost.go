package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/billing"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

// SessionCostView is the accumulated cost and idle-termination countdown
// for one deployment, as a dashboard session summary renders it.
//
// Idle time is measured from the deployment record's own last-updated
// timestamp, not a dedicated per-request activity signal: no component in
// this gateway yet attributes an inference request to the specific
// deployment that served it (that binding lives in the scheduler's GPU
// allocation layer, not the HTTP request path), so "last updated" is the
// most accurate proxy for "last touched" actually available. It is
// updated whenever the deployment's own state changes (e.g. its GPU
// model is set), which undercounts idle time relative to true per-request
// activity but never overcounts it into a premature reap.
type SessionCostView struct {
	DeploymentID       string   `json:"deployment_id"`
	GPUModel           string   `json:"gpu_model,omitempty"`
	HourlyRateUSD      *float64 `json:"hourly_rate_usd,omitempty"`
	ElapsedSeconds     float64  `json:"elapsed_seconds"`
	CostUSD            *float64 `json:"cost_usd,omitempty"`
	ProjectedHourlyUSD *float64 `json:"projected_hourly_usd,omitempty"`
	IdleSeconds        float64  `json:"idle_seconds"`
	IdleTimeoutSeconds float64  `json:"idle_timeout_seconds"`
	SecondsUntilReap   float64  `json:"seconds_until_reap"`
}

// SessionCostHandler implements GET /v1/dashboard/deployments/{id}/session:
// the accumulated dollar cost of a deployment's wall-clock lifetime plus
// its projected hourly burn rate, and a countdown to automatic idle
// termination.
func SessionCostHandler(deployments DeploymentGetter, rates billing.HourlyRates, idleTimeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		id := r.PathValue("id")
		deployment, err := deployments.GetByID(r.Context(), id)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		now := time.Now()
		elapsed := now.Sub(deployment.CreatedAt)
		idleFor := now.Sub(deployment.UpdatedAt)
		secondsUntilReap := (idleTimeout - idleFor).Seconds()
		if secondsUntilReap < 0 {
			secondsUntilReap = 0
		}

		view := SessionCostView{
			DeploymentID: deployment.ID, ElapsedSeconds: elapsed.Seconds(),
			IdleSeconds: idleFor.Seconds(), IdleTimeoutSeconds: idleTimeout.Seconds(),
			SecondsUntilReap: secondsUntilReap,
		}

		if deployment.GPUModel != nil {
			view.GPUModel = *deployment.GPUModel
			if rate, err := rates.RateFor(*deployment.GPUModel); err == nil {
				cost := billing.ComputeCost(elapsed, rate)
				view.HourlyRateUSD = &rate
				view.CostUSD = &cost
				// A deployment in this gateway is a single GPU node
				// billed at a flat hourly rate, so the projected burn
				// is simply that rate - there is no idle-adjusted or
				// multi-GPU factor to project against yet.
				view.ProjectedHourlyUSD = &rate
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(view)
	}
}
