package scheduler

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// RetryPolicy governs whether a failed deployment attempt should be
// retried, and if so, how long to wait first.
type RetryPolicy struct {
	// BaseDelay is the backoff for the first retry.
	BaseDelay time.Duration
	// MaxDelay caps the backoff regardless of attempt count.
	MaxDelay time.Duration
	// MaxAttempts is the total number of attempts allowed (including the
	// first), beyond which the deployment fails permanently even for an
	// otherwise-retryable failure class.
	MaxAttempts int
}

// DefaultRetryPolicy backs off from 2s up to a 5 minute cap over at most
// 8 attempts - generous enough to ride out transient placement pressure
// without retrying forever.
var DefaultRetryPolicy = RetryPolicy{
	BaseDelay:   2 * time.Second,
	MaxDelay:    5 * time.Minute,
	MaxAttempts: 8,
}

func (p RetryPolicy) normalize() RetryPolicy {
	if p.BaseDelay <= 0 {
		p.BaseDelay = DefaultRetryPolicy.BaseDelay
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = DefaultRetryPolicy.MaxDelay
	}
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = DefaultRetryPolicy.MaxAttempts
	}
	return p
}

// IsRetryable reports whether a failure of this class is ever worth
// retrying. Only placement failures are: they reflect transient cluster
// resource pressure that another attempt, possibly against a node whose
// resources have since freed up, can plausibly resolve. An image pull
// failure needs a human to fix the reference, an OOM kill needs a
// smaller footprint or bigger node, and a plain task crash's cause is
// unknown - none of those are fixed by simply trying again.
func (p RetryPolicy) IsRetryable(class FailureClass) bool {
	return class == FailurePlacement
}

// RetryDecision is the outcome of RetryPolicy.Decide.
type RetryDecision struct {
	Retry  bool
	Delay  time.Duration
	Reason string
}

// Decide determines whether attempt (1-indexed: the attempt that just
// failed) should be retried given failure, and if so, after how long.
// failure == nil is treated as no failure to react to.
func (p RetryPolicy) Decide(failure *AllocationFailureError, attempt int) RetryDecision {
	if failure == nil {
		return RetryDecision{Retry: false, Reason: "no failure"}
	}

	p = p.normalize()

	if !p.IsRetryable(failure.Class) {
		return RetryDecision{Retry: false, Reason: fmt.Sprintf("non-retryable failure class %q", failure.Class)}
	}
	if attempt >= p.MaxAttempts {
		return RetryDecision{Retry: false, Reason: fmt.Sprintf("exhausted %d attempts", p.MaxAttempts)}
	}

	return RetryDecision{Retry: true, Delay: p.NextDelay(attempt)}
}

// NextDelay computes the backoff before retrying after the given attempt
// (1-indexed), as capped exponential backoff with full jitter: a value
// drawn uniformly from [0, cap], where cap doubles per attempt up to
// MaxDelay. Full jitter (rather than a fixed delay or additive jitter)
// spreads retries across the whole window, avoiding synchronized retry
// storms when many deployments fail at once.
func (p RetryPolicy) NextDelay(attempt int) time.Duration {
	p = p.normalize()

	if attempt < 1 {
		attempt = 1
	}

	cap64 := int64(p.MaxDelay)
	base := int64(p.BaseDelay)

	// Compute base * 2^(attempt-1), saturating at cap64 rather than
	// overflowing for a large attempt count.
	backoff := base
	for i := 1; i < attempt && backoff < cap64; i++ {
		backoff *= 2
		if backoff <= 0 { // overflow
			backoff = cap64
			break
		}
	}
	if backoff > cap64 {
		backoff = cap64
	}
	if backoff <= 0 {
		return 0
	}

	return time.Duration(rand.Int64N(backoff + 1)) // #nosec G404 -- jitter timing, not a security-sensitive value
}
