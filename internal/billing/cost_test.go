package billing_test

import (
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/billing"
)

func TestComputeCost(t *testing.T) {
	cases := []struct {
		wallTime time.Duration
		rate     float64
		want     float64
	}{
		{wallTime: time.Hour, rate: 4.0, want: 4.0},
		{wallTime: 30 * time.Minute, rate: 4.0, want: 2.0},
		{wallTime: 90 * time.Minute, rate: 2.0, want: 3.0},
		{wallTime: 0, rate: 4.0, want: 0},
	}
	for _, tc := range cases {
		got := billing.ComputeCost(tc.wallTime, tc.rate)
		if got != tc.want {
			t.Errorf("ComputeCost(%v, %v) = %v, want %v", tc.wallTime, tc.rate, got, tc.want)
		}
	}
}

func TestHourlyRates_RateFor_Known(t *testing.T) {
	rates := billing.HourlyRates{"A100": 4.10, "H100": 8.20}

	rate, err := rates.RateFor("A100")
	if err != nil {
		t.Fatalf("RateFor() error = %v", err)
	}
	if rate != 4.10 {
		t.Errorf("RateFor(A100) = %v, want 4.10", rate)
	}
}

func TestHourlyRates_RateFor_Unknown(t *testing.T) {
	rates := billing.HourlyRates{"A100": 4.10}

	_, err := rates.RateFor("H200")
	if !errors.Is(err, billing.ErrUnknownGPUModel) {
		t.Fatalf("RateFor(H200) error = %v, want ErrUnknownGPUModel", err)
	}
}
