package policy

import (
	"context"
	"fmt"
	"time"
)

// ViolationCounter atomically increments and returns a user's abuse
// violation count within a rolling window, and sets that window's expiry.
// It is satisfied by *RedisViolationCounter.
type ViolationCounter interface {
	Incr(ctx context.Context, key string) (int64, error)
	Expire(ctx context.Context, key string, ttl time.Duration) error
}

// EscalationLevel is how far an account's automatic abuse response has
// progressed.
type EscalationLevel string

const (
	// EscalationNone means the account is below the reduction threshold - no action taken.
	EscalationNone EscalationLevel = "none"
	// EscalationReduced means the account's effective rate limit has been cut.
	EscalationReduced EscalationLevel = "reduced"
	// EscalationSuspended means the account has been suspended outright.
	EscalationSuspended EscalationLevel = "suspended"
)

// RateReducer records that userID's effective rate limit should be
// reduced. It is satisfied by *RateReductionStore.
type RateReducer interface {
	Reduce(ctx context.Context, userID string, ttl time.Duration) error
}

// Suspender immediately suspends an account. It is satisfied by
// *Enforcer.
type Suspender interface {
	Suspend(ctx context.Context, userID string) (*Result, error)
}

// EscalationThresholds sets how many accumulated violations move an
// account from one EscalationLevel to the next.
type EscalationThresholds struct {
	// ReduceAt is the violation count at which the account's rate limit
	// is cut.
	ReduceAt int64
	// SuspendAt is the violation count at which the account is suspended
	// outright - always >= ReduceAt, since suspension is the harsher
	// response and must never trigger before reduction would have.
	SuspendAt int64
	// ViolationWindow bounds how long violations accumulate before the
	// count resets - an account that stops triggering signals is not
	// punished forever for a past burst.
	ViolationWindow time.Duration
	// ReducedRateTTL is how long a rate reduction stays in effect once
	// applied.
	ReducedRateTTL time.Duration
}

// DefaultEscalationThresholds reduce after 3 flagged signals within an
// hour, and suspend outright after 6 within the same window.
var DefaultEscalationThresholds = EscalationThresholds{
	ReduceAt: 3, SuspendAt: 6,
	ViolationWindow: time.Hour,
	ReducedRateTTL:  time.Hour,
}

// Escalator turns a stream of flagged abuse signals (from AbuseDetector)
// into an escalating automatic response: accumulate violations, cut the
// rate limit once a threshold is crossed, and suspend the account
// outright if the behavior continues past a second, harsher threshold.
type Escalator struct {
	violations ViolationCounter
	reducer    RateReducer
	suspender  Suspender
	thresholds EscalationThresholds
}

// NewEscalator returns an Escalator wired to its dependencies, using
// thresholds (DefaultEscalationThresholds if the zero value).
func NewEscalator(violations ViolationCounter, reducer RateReducer, suspender Suspender, thresholds EscalationThresholds) *Escalator {
	if thresholds.ReduceAt <= 0 {
		thresholds = DefaultEscalationThresholds
	}
	return &Escalator{violations: violations, reducer: reducer, suspender: suspender, thresholds: thresholds}
}

// RecordViolation records one flagged signal for userID and applies
// whatever escalation level its now-updated violation count crosses into.
// Crossing SuspendAt always also implies (and skips redundantly applying)
// the reduction a lower count would have triggered - suspension already
// blocks every future request regardless.
func (e *Escalator) RecordViolation(ctx context.Context, userID string) (EscalationLevel, error) {
	key := violationKey(userID)
	count, err := e.violations.Incr(ctx, key)
	if err != nil {
		return EscalationNone, fmt.Errorf("policy: increment violation count for %s: %w", userID, err)
	}
	if count == 1 {
		if err := e.violations.Expire(ctx, key, e.thresholds.ViolationWindow); err != nil {
			return EscalationNone, fmt.Errorf("policy: set violation window for %s: %w", userID, err)
		}
	}

	switch {
	case count >= e.thresholds.SuspendAt:
		if _, err := e.suspender.Suspend(ctx, userID); err != nil {
			return EscalationNone, fmt.Errorf("policy: suspend %s at violation count %d: %w", userID, count, err)
		}
		return EscalationSuspended, nil
	case count >= e.thresholds.ReduceAt:
		if err := e.reducer.Reduce(ctx, userID, e.thresholds.ReducedRateTTL); err != nil {
			return EscalationNone, fmt.Errorf("policy: reduce rate for %s at violation count %d: %w", userID, count, err)
		}
		return EscalationReduced, nil
	default:
		return EscalationNone, nil
	}
}

func violationKey(userID string) string { return "abuse:violations:" + userID }
