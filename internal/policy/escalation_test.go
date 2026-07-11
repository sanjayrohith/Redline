package policy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/policy"
)

type fakeViolationCounter struct {
	counts map[string]int64
}

func newFakeViolationCounter() *fakeViolationCounter {
	return &fakeViolationCounter{counts: map[string]int64{}}
}

func (f *fakeViolationCounter) Incr(_ context.Context, key string) (int64, error) {
	f.counts[key]++
	return f.counts[key], nil
}

func (f *fakeViolationCounter) Expire(context.Context, string, time.Duration) error { return nil }

type fakeRateReducer struct {
	reducedFor []string
}

func (f *fakeRateReducer) Reduce(_ context.Context, userID string, _ time.Duration) error {
	f.reducedFor = append(f.reducedFor, userID)
	return nil
}

type fakeSuspender struct {
	suspendedFor []string
	err          error
}

func (f *fakeSuspender) Suspend(_ context.Context, userID string) (*policy.Result, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.suspendedFor = append(f.suspendedFor, userID)
	return &policy.Result{}, nil
}

func testThresholds() policy.EscalationThresholds {
	return policy.EscalationThresholds{ReduceAt: 2, SuspendAt: 4, ViolationWindow: time.Hour, ReducedRateTTL: time.Hour}
}

func TestEscalator_BelowThreshold_NoAction(t *testing.T) {
	reducer := &fakeRateReducer{}
	suspender := &fakeSuspender{}
	esc := policy.NewEscalator(newFakeViolationCounter(), reducer, suspender, testThresholds())

	level, err := esc.RecordViolation(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("RecordViolation() error = %v", err)
	}
	if level != policy.EscalationNone {
		t.Errorf("level = %q, want none", level)
	}
	if len(reducer.reducedFor) != 0 || len(suspender.suspendedFor) != 0 {
		t.Error("no action should have been taken below the reduce threshold")
	}
}

func TestEscalator_ReachesReduceThreshold(t *testing.T) {
	reducer := &fakeRateReducer{}
	suspender := &fakeSuspender{}
	esc := policy.NewEscalator(newFakeViolationCounter(), reducer, suspender, testThresholds())

	var level policy.EscalationLevel
	for i := 0; i < 2; i++ {
		var err error
		level, err = esc.RecordViolation(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("RecordViolation() error = %v", err)
		}
	}

	if level != policy.EscalationReduced {
		t.Errorf("level = %q, want reduced", level)
	}
	if len(reducer.reducedFor) != 1 || reducer.reducedFor[0] != "user-1" {
		t.Errorf("reducedFor = %v, want [user-1]", reducer.reducedFor)
	}
	if len(suspender.suspendedFor) != 0 {
		t.Error("account must not be suspended yet at the reduce threshold")
	}
}

func TestEscalator_ReachesSuspendThreshold(t *testing.T) {
	reducer := &fakeRateReducer{}
	suspender := &fakeSuspender{}
	esc := policy.NewEscalator(newFakeViolationCounter(), reducer, suspender, testThresholds())

	var level policy.EscalationLevel
	for i := 0; i < 4; i++ {
		var err error
		level, err = esc.RecordViolation(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("RecordViolation() error = %v", err)
		}
	}

	if level != policy.EscalationSuspended {
		t.Errorf("level = %q, want suspended", level)
	}
	if len(suspender.suspendedFor) != 1 || suspender.suspendedFor[0] != "user-1" {
		t.Errorf("suspendedFor = %v, want [user-1]", suspender.suspendedFor)
	}
}

func TestEscalator_UsersAreIndependent(t *testing.T) {
	reducer := &fakeRateReducer{}
	suspender := &fakeSuspender{}
	esc := policy.NewEscalator(newFakeViolationCounter(), reducer, suspender, testThresholds())

	if _, err := esc.RecordViolation(context.Background(), "user-1"); err != nil {
		t.Fatalf("RecordViolation() error = %v", err)
	}
	level, err := esc.RecordViolation(context.Background(), "user-2")
	if err != nil {
		t.Fatalf("RecordViolation() error = %v", err)
	}
	if level != policy.EscalationNone {
		t.Errorf("user-2 level = %q, want none (independent from user-1's count)", level)
	}
}

func TestEscalator_SuspensionFailurePropagates(t *testing.T) {
	suspender := &fakeSuspender{err: errors.New("db unreachable")}
	esc := policy.NewEscalator(newFakeViolationCounter(), &fakeRateReducer{}, suspender, testThresholds())

	var err error
	for i := 0; i < 4; i++ {
		_, err = esc.RecordViolation(context.Background(), "user-1")
	}
	if err == nil {
		t.Fatal("RecordViolation() error = nil, want an error when Suspend fails at the suspend threshold")
	}
}
