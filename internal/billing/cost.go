// Package billing attributes a concrete dollar cost to inference work,
// derived from wall-clock allocation time and the node's configured
// hourly rate - the same unit cloud GPU providers themselves bill in.
package billing

import (
	"errors"
	"fmt"
	"time"
)

// ErrUnknownGPUModel means no hourly rate is configured for a given GPU
// model, so no cost can be attributed to work that ran on it.
var ErrUnknownGPUModel = errors.New("billing: no hourly rate configured for this gpu model")

// HourlyRates maps a GPU model identifier (as recorded on
// deployments.gpu_model) to its configured cost in USD per hour of
// allocation wall time.
type HourlyRates map[string]float64

// RateFor returns the hourly rate configured for gpuModel, or
// ErrUnknownGPUModel if none is configured - refusing to attribute a
// silent zero cost to hardware nobody priced, since that would make
// every rollup relying on it look artificially cheap rather than
// visibly wrong.
func (r HourlyRates) RateFor(gpuModel string) (float64, error) {
	rate, ok := r[gpuModel]
	if !ok {
		return 0, fmt.Errorf("%w: %q", ErrUnknownGPUModel, gpuModel)
	}
	return rate, nil
}

// ComputeCost attributes a dollar cost to wallTime of allocation at
// hourlyRate: simply wallTime's fraction of an hour times the rate, the
// same arithmetic a cloud GPU bill itself uses.
func ComputeCost(wallTime time.Duration, hourlyRate float64) float64 {
	return wallTime.Hours() * hourlyRate
}
