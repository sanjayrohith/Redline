package policy_test

import (
	"context"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/policy"
)

type fakeSignalLimiter struct {
	callsFor map[string]int
	limitFor map[string]int
}

func newFakeSignalLimiter() *fakeSignalLimiter {
	return &fakeSignalLimiter{callsFor: map[string]int{}, limitFor: map[string]int{}}
}

func (f *fakeSignalLimiter) Allow(_ context.Context, key string, limit int, _ time.Duration) (bool, error) {
	f.callsFor[key]++
	return f.callsFor[key] <= limit, nil
}

func TestAbuseDetector_CheckRequestCadence_FlagsPastLimit(t *testing.T) {
	limiter := newFakeSignalLimiter()
	detector := policy.NewAbuseDetector(limiter, policy.AbuseThresholds{
		CadenceLimit: 2, CadenceWindow: time.Minute,
		LowEntropyBurstLimit: 100, LowEntropyWindow: time.Minute, EntropyThreshold: policy.LowEntropyThreshold,
		AllocationChurnLimit: 100, AllocationChurnWindow: time.Minute,
	})

	for i := 0; i < 2; i++ {
		flagged, err := detector.CheckRequestCadence(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("CheckRequestCadence() error = %v", err)
		}
		if flagged {
			t.Fatalf("request %d flagged too early", i+1)
		}
	}

	flagged, err := detector.CheckRequestCadence(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("CheckRequestCadence() error = %v", err)
	}
	if !flagged {
		t.Error("3rd request within a 2-request budget should have flagged")
	}
}

func TestAbuseDetector_CheckPromptEntropy_OnlyLowEntropyCountsAgainstBudget(t *testing.T) {
	limiter := newFakeSignalLimiter()
	detector := policy.NewAbuseDetector(limiter, policy.AbuseThresholds{
		CadenceLimit: 1000, CadenceWindow: time.Minute,
		LowEntropyBurstLimit: 1, LowEntropyWindow: time.Minute, EntropyThreshold: policy.LowEntropyThreshold,
		AllocationChurnLimit: 1000, AllocationChurnWindow: time.Minute,
	})

	// Natural-language prompts never touch the low-entropy budget.
	for i := 0; i < 5; i++ {
		flagged, err := detector.CheckPromptEntropy(context.Background(), "user-1", "please summarize this quarterly report for me")
		if err != nil {
			t.Fatalf("CheckPromptEntropy() error = %v", err)
		}
		if flagged {
			t.Fatalf("natural-language prompt %d should never flag", i+1)
		}
	}

	// A single low-entropy prompt is within budget (limit 1)...
	flagged, err := detector.CheckPromptEntropy(context.Background(), "user-1", "aaaaaaaaaa")
	if err != nil {
		t.Fatalf("CheckPromptEntropy() error = %v", err)
	}
	if flagged {
		t.Fatal("first low-entropy prompt flagged too early")
	}

	// ...but a second one exceeds it.
	flagged, err = detector.CheckPromptEntropy(context.Background(), "user-1", "bbbbbbbbbb")
	if err != nil {
		t.Fatalf("CheckPromptEntropy() error = %v", err)
	}
	if !flagged {
		t.Error("second low-entropy prompt within the burst window should have flagged")
	}
}

func TestAbuseDetector_CheckAllocationChurn_FlagsPastLimit(t *testing.T) {
	limiter := newFakeSignalLimiter()
	detector := policy.NewAbuseDetector(limiter, policy.AbuseThresholds{
		CadenceLimit: 1000, CadenceWindow: time.Minute,
		LowEntropyBurstLimit: 1000, LowEntropyWindow: time.Minute, EntropyThreshold: policy.LowEntropyThreshold,
		AllocationChurnLimit: 1, AllocationChurnWindow: time.Minute,
	})

	if flagged, err := detector.CheckAllocationChurn(context.Background(), "user-1"); err != nil || flagged {
		t.Fatalf("first churn event flagged = %v, err = %v, want false", flagged, err)
	}
	flagged, err := detector.CheckAllocationChurn(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("CheckAllocationChurn() error = %v", err)
	}
	if !flagged {
		t.Error("second churn event within a 1-event budget should have flagged")
	}
}

func TestAbuseDetector_SignalsAreIndependentPerUser(t *testing.T) {
	limiter := newFakeSignalLimiter()
	detector := policy.NewAbuseDetector(limiter, policy.AbuseThresholds{
		CadenceLimit: 1, CadenceWindow: time.Minute,
		LowEntropyBurstLimit: 1000, LowEntropyWindow: time.Minute, EntropyThreshold: policy.LowEntropyThreshold,
		AllocationChurnLimit: 1000, AllocationChurnWindow: time.Minute,
	})

	if flagged, _ := detector.CheckRequestCadence(context.Background(), "user-1"); flagged {
		t.Fatal("user-1's first request flagged too early")
	}
	if flagged, _ := detector.CheckRequestCadence(context.Background(), "user-2"); flagged {
		t.Error("user-2's traffic must not be affected by user-1's budget")
	}
}
