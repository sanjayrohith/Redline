// Package health implements the gateway's liveness and readiness checks.
package health

import (
	"context"
	"encoding/json"
	"net/http"
)

// Check is one named dependency probe. Fn returning a non-nil error marks
// that dependency down.
type Check struct {
	Name string
	Fn   func(ctx context.Context) error
}

// Aggregator evaluates every registered Check and reports a single
// dependency-aware verdict.
type Aggregator struct {
	checks []Check
}

// NewAggregator returns an Aggregator that evaluates checks in order.
func NewAggregator(checks ...Check) *Aggregator {
	return &Aggregator{checks: checks}
}

// Result is the outcome of one Check.
type Result struct {
	Name  string `json:"name"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// Report is the aggregate outcome across every registered Check.
type Report struct {
	OK     bool     `json:"ok"`
	Checks []Result `json:"checks"`
}

// Evaluate runs every check against ctx and returns the aggregate report.
func (a *Aggregator) Evaluate(ctx context.Context) Report {
	report := Report{OK: true, Checks: make([]Result, 0, len(a.checks))}

	for _, c := range a.checks {
		result := Result{Name: c.Name, OK: true}
		if err := c.Fn(ctx); err != nil {
			result.OK = false
			result.Error = err.Error()
			report.OK = false
		}
		report.Checks = append(report.Checks, result)
	}

	return report
}

// LivenessHandler reports process liveness unconditionally: if the process
// can run this handler, it is alive.
func LivenessHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ReadinessHandler reports whether every dependency in agg is reachable,
// returning 503 if any check fails.
func ReadinessHandler(agg *Aggregator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		report := agg.Evaluate(r.Context())

		status := http.StatusOK
		if !report.OK {
			status = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(report)
	}
}
