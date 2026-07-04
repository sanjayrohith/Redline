package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/billing"
	"github.com/sanjayrohith/redline/internal/db"
)

func TestSessionCostHandler_ComputesCostForKnownGPUModel(t *testing.T) {
	gpuModel := "A100"
	createdAt := time.Now().Add(-2 * time.Hour)
	updatedAt := time.Now().Add(-5 * time.Minute)
	deployments := &fakeDeploymentGetter{byID: map[string]*db.Deployment{
		"dep-1": {ID: "dep-1", GPUModel: &gpuModel, CreatedAt: createdAt, UpdatedAt: updatedAt},
	}}
	rates := billing.HourlyRates{"A100": 2.5}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/dashboard/deployments/{id}/session", SessionCostHandler(deployments, rates, 15*time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/deployments/dep-1/session", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	var view SessionCostView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.HourlyRateUSD == nil || *view.HourlyRateUSD != 2.5 {
		t.Errorf("HourlyRateUSD = %v, want 2.5", view.HourlyRateUSD)
	}
	if view.CostUSD == nil || *view.CostUSD < 4.9 || *view.CostUSD > 5.1 {
		t.Errorf("CostUSD = %v, want ~5.0 (2h at $2.50/hr)", view.CostUSD)
	}
	if view.SecondsUntilReap <= 0 {
		t.Errorf("SecondsUntilReap = %f, want > 0 (idle 5m against a 15m timeout)", view.SecondsUntilReap)
	}
}

func TestSessionCostHandler_ReapCountdownReachesZeroPastTimeout(t *testing.T) {
	gpuModel := "A100"
	deployments := &fakeDeploymentGetter{byID: map[string]*db.Deployment{
		"dep-1": {ID: "dep-1", GPUModel: &gpuModel, CreatedAt: time.Now(), UpdatedAt: time.Now().Add(-30 * time.Minute)},
	}}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/dashboard/deployments/{id}/session",
		SessionCostHandler(deployments, billing.HourlyRates{"A100": 2.5}, 15*time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/deployments/dep-1/session", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var view SessionCostView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.SecondsUntilReap != 0 {
		t.Errorf("SecondsUntilReap = %f, want 0 once idle time exceeds the timeout", view.SecondsUntilReap)
	}
}

func TestSessionCostHandler_UnknownGPUModelOmitsCost(t *testing.T) {
	gpuModel := "some-unpriced-card"
	deployments := &fakeDeploymentGetter{byID: map[string]*db.Deployment{
		"dep-1": {ID: "dep-1", GPUModel: &gpuModel, CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/dashboard/deployments/{id}/session",
		SessionCostHandler(deployments, billing.HourlyRates{"A100": 2.5}, 15*time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/deployments/dep-1/session", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var view SessionCostView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.CostUSD != nil {
		t.Errorf("CostUSD = %v, want nil for an unpriced GPU model", view.CostUSD)
	}
}

func TestSessionCostHandler_UnknownDeploymentIsNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/dashboard/deployments/{id}/session",
		SessionCostHandler(&fakeDeploymentGetter{byID: map[string]*db.Deployment{}}, billing.HourlyRates{}, time.Minute))

	req := httptest.NewRequest(http.MethodGet, "/v1/dashboard/deployments/nope/session", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
