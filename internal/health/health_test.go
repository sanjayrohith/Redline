package health

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLivenessHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	LivenessHandler(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadinessHandler_AllHealthy(t *testing.T) {
	agg := NewAggregator(
		Check{Name: "postgres", Fn: func(context.Context) error { return nil }},
		Check{Name: "redis", Fn: func(context.Context) error { return nil }},
	)

	rec := httptest.NewRecorder()
	ReadinessHandler(agg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var report Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !report.OK || len(report.Checks) != 2 {
		t.Errorf("report = %+v", report)
	}
}

func TestReadinessHandler_OneDependencyDown(t *testing.T) {
	agg := NewAggregator(
		Check{Name: "postgres", Fn: func(context.Context) error { return nil }},
		Check{Name: "redis", Fn: func(context.Context) error { return errors.New("connection refused") }},
	)

	rec := httptest.NewRecorder()
	ReadinessHandler(agg).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}

	var report Report
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if report.OK {
		t.Error("report.OK = true, want false")
	}

	var redisResult *Result
	for i := range report.Checks {
		if report.Checks[i].Name == "redis" {
			redisResult = &report.Checks[i]
		}
	}
	if redisResult == nil || redisResult.OK || redisResult.Error == "" {
		t.Errorf("redis result = %+v", redisResult)
	}
}
