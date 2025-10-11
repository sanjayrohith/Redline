package scheduler

import (
	"testing"
	"time"
)

func TestRetryPolicy_IsRetryable(t *testing.T) {
	p := DefaultRetryPolicy

	if !p.IsRetryable(FailurePlacement) {
		t.Error("FailurePlacement should be retryable")
	}
	for _, class := range []FailureClass{FailureImagePull, FailureOOMKilled, FailureTaskCrash, FailureUnknown} {
		if p.IsRetryable(class) {
			t.Errorf("%s should not be retryable", class)
		}
	}
}

func TestRetryPolicy_Decide_NonRetryableClassFailsImmediately(t *testing.T) {
	p := DefaultRetryPolicy
	failure := &AllocationFailureError{Class: FailureImagePull}

	decision := p.Decide(failure, 1)
	if decision.Retry {
		t.Error("Retry = true, want false for a non-retryable class")
	}
	if decision.Reason == "" {
		t.Error("Reason should explain why retry was refused")
	}
}

func TestRetryPolicy_Decide_RetryableClassRetriesUntilMaxAttempts(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: time.Minute, MaxAttempts: 3}
	failure := &AllocationFailureError{Class: FailurePlacement}

	for attempt := 1; attempt < 3; attempt++ {
		decision := p.Decide(failure, attempt)
		if !decision.Retry {
			t.Errorf("attempt %d: Retry = false, want true", attempt)
		}
	}

	decision := p.Decide(failure, 3)
	if decision.Retry {
		t.Error("attempt 3 (== MaxAttempts): Retry = true, want false")
	}
}

func TestRetryPolicy_Decide_NilFailure(t *testing.T) {
	decision := DefaultRetryPolicy.Decide(nil, 1)
	if decision.Retry {
		t.Error("Retry = true, want false for a nil failure")
	}
}

func TestRetryPolicy_NextDelay_BoundedByCap(t *testing.T) {
	p := RetryPolicy{BaseDelay: 100 * time.Millisecond, MaxDelay: time.Second, MaxAttempts: 10}

	for attempt := 1; attempt <= 20; attempt++ {
		for i := 0; i < 50; i++ { // sample many draws since jitter is random
			delay := p.NextDelay(attempt)
			if delay < 0 || delay > p.MaxDelay {
				t.Fatalf("attempt %d: delay = %v, want in [0, %v]", attempt, delay, p.MaxDelay)
			}
		}
	}
}

func TestRetryPolicy_NextDelay_CapGrowsWithAttempt(t *testing.T) {
	p := RetryPolicy{BaseDelay: 10 * time.Millisecond, MaxDelay: time.Hour, MaxAttempts: 10}

	maxObserved := func(attempt int, samples int) time.Duration {
		var max time.Duration
		for i := 0; i < samples; i++ {
			if d := p.NextDelay(attempt); d > max {
				max = d
			}
		}
		return max
	}

	// With enough samples, the observed max for a later attempt should
	// exceed that of an earlier attempt, since the underlying cap doubles.
	early := maxObserved(1, 200)
	later := maxObserved(5, 200)

	if later <= early {
		t.Errorf("later-attempt max delay (%v) should exceed early-attempt max delay (%v)", later, early)
	}
}

func TestRetryPolicy_NextDelay_JitterVaries(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: time.Minute, MaxAttempts: 10}

	seen := map[time.Duration]bool{}
	for i := 0; i < 20; i++ {
		seen[p.NextDelay(4)] = true
	}
	if len(seen) < 2 {
		t.Error("NextDelay should produce varying values across calls (jitter), got all-identical results")
	}
}

func TestRetryPolicy_ZeroValueUsesDefaults(t *testing.T) {
	var p RetryPolicy
	failure := &AllocationFailureError{Class: FailurePlacement}

	decision := p.Decide(failure, 1)
	if !decision.Retry {
		t.Error("zero-value RetryPolicy should fall back to DefaultRetryPolicy and allow a retry on attempt 1")
	}
}
