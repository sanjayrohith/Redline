package policy

import (
	"context"
	"fmt"
	"time"
)

// SignalLimiter is the windowed-counter dependency AbuseDetector needs
// for each signal it tracks. It is satisfied by *ratelimit.Limiter - the
// same sliding-window primitive the gateway's own rate limiting uses,
// reused here at different (much stricter) thresholds to detect abuse
// rather than merely bound normal traffic.
type SignalLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (Allowed bool, err error)
}

// limiterAdapter adapts *ratelimit.Limiter's richer Decision return to
// the narrow bool SignalLimiter needs.
type limiterAdapter struct {
	allow func(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

func (a limiterAdapter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	return a.allow(ctx, key, limit, window)
}

// NewSignalLimiterFunc adapts a ratelimit.Limiter-shaped Allow method
// (returning ratelimit.Decision) into a SignalLimiter.
func NewSignalLimiterFunc(allow func(ctx context.Context, key string, limit int, window time.Duration) (bool, error)) SignalLimiter {
	return limiterAdapter{allow: allow}
}

// AbuseThresholds configures how aggressively AbuseDetector flags each
// signal. Every threshold is deliberately far looser than what a normal
// interactive user would ever hit, and far tighter than what a scripted
// client cycling requests as fast as possible would stay under.
type AbuseThresholds struct {
	// CadenceLimit bounds requests per CadenceWindow before the cadence
	// signal flags.
	CadenceLimit  int
	CadenceWindow time.Duration
	// LowEntropyBurstLimit bounds how many low-entropy (repetitive,
	// scripted-looking) prompts a principal may send per
	// LowEntropyWindow before the entropy signal flags.
	LowEntropyBurstLimit int
	LowEntropyWindow     time.Duration
	// EntropyThreshold is the Shannon-entropy cutoff a prompt must clear
	// to count as normal rather than low-entropy.
	EntropyThreshold float64
	// AllocationChurnLimit bounds deployment create/terminate events per
	// AllocationChurnWindow before the churn signal flags.
	AllocationChurnLimit  int
	AllocationChurnWindow time.Duration
}

// DefaultAbuseThresholds are generous enough that no legitimate
// interactive or moderate-automation workload should ever trip them, and
// tight enough that a scripted scraping or spam client sustaining
// anywhere near its actual capacity will.
var DefaultAbuseThresholds = AbuseThresholds{
	CadenceLimit:  120,
	CadenceWindow: time.Minute,

	LowEntropyBurstLimit: 20,
	LowEntropyWindow:     time.Minute,
	EntropyThreshold:     LowEntropyThreshold,

	AllocationChurnLimit:  10,
	AllocationChurnWindow: time.Minute,
}

// Signal names the abuse pattern a detection flagged.
type Signal string

const (
	// SignalRequestCadence flags a request-rate burst.
	SignalRequestCadence Signal = "request_cadence"
	// SignalPromptEntropy flags a burst of low-entropy (scripted-looking) prompts.
	SignalPromptEntropy Signal = "prompt_entropy"
	// SignalAllocationChurn flags a deployment create/terminate burst.
	SignalAllocationChurn Signal = "allocation_churn"
)

// AbuseDetector flags a principal's traffic against three independent
// signals - request cadence, prompt entropy, and allocation churn - any
// one of which crossing its threshold is itself a flag; they are not
// combined into a single score, since each is sufficient evidence of
// abuse on its own.
type AbuseDetector struct {
	limiter    SignalLimiter
	thresholds AbuseThresholds
}

// NewAbuseDetector returns an AbuseDetector backed by limiter, using
// thresholds (DefaultAbuseThresholds if the zero value).
func NewAbuseDetector(limiter SignalLimiter, thresholds AbuseThresholds) *AbuseDetector {
	if thresholds.CadenceLimit <= 0 {
		thresholds = DefaultAbuseThresholds
	}
	return &AbuseDetector{limiter: limiter, thresholds: thresholds}
}

// CheckRequestCadence records one request for userID and reports whether
// its request cadence just exceeded CadenceLimit within CadenceWindow.
func (d *AbuseDetector) CheckRequestCadence(ctx context.Context, userID string) (bool, error) {
	allowed, err := d.limiter.Allow(ctx, cadenceKey(userID), d.thresholds.CadenceLimit, d.thresholds.CadenceWindow)
	if err != nil {
		return false, fmt.Errorf("policy: check request cadence for %s: %w", userID, err)
	}
	return !allowed, nil
}

// CheckPromptEntropy scores prompt's Shannon entropy; if it is low, this
// counts one low-entropy prompt against userID's burst budget and reports
// whether that budget was just exceeded. A single low-entropy prompt is
// not itself abuse (a legitimate short or templated request can score
// low); a sustained burst of them is.
func (d *AbuseDetector) CheckPromptEntropy(ctx context.Context, userID, prompt string) (bool, error) {
	if !IsLowEntropy(prompt, d.thresholds.EntropyThreshold) {
		return false, nil
	}
	allowed, err := d.limiter.Allow(ctx, entropyKey(userID), d.thresholds.LowEntropyBurstLimit, d.thresholds.LowEntropyWindow)
	if err != nil {
		return false, fmt.Errorf("policy: check prompt entropy for %s: %w", userID, err)
	}
	return !allowed, nil
}

// CheckAllocationChurn records one deployment create/terminate event for
// userID and reports whether its churn rate just exceeded
// AllocationChurnLimit within AllocationChurnWindow.
func (d *AbuseDetector) CheckAllocationChurn(ctx context.Context, userID string) (bool, error) {
	allowed, err := d.limiter.Allow(ctx, churnKey(userID), d.thresholds.AllocationChurnLimit, d.thresholds.AllocationChurnWindow)
	if err != nil {
		return false, fmt.Errorf("policy: check allocation churn for %s: %w", userID, err)
	}
	return !allowed, nil
}

func cadenceKey(userID string) string { return "abuse:cadence:" + userID }
func entropyKey(userID string) string { return "abuse:entropy:" + userID }
func churnKey(userID string) string   { return "abuse:churn:" + userID }
