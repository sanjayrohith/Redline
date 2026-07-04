package resilience_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/resilience"
)

type fakeCanceller struct {
	cancelled []string
}

func (f *fakeCanceller) Cancel(_ context.Context, requestID string) error {
	f.cancelled = append(f.cancelled, requestID)
	return nil
}

type fakeRequeuer struct {
	enqueued []string
	err      error
}

func (f *fakeRequeuer) Enqueue(_ context.Context, id, _ string) error {
	if f.err != nil {
		return f.err
	}
	f.enqueued = append(f.enqueued, id)
	return nil
}

func TestReaper_Sweep_CancelsAndRequeuesStuckRequests(t *testing.T) {
	w := resilience.NewWatchdog(5 * time.Millisecond)
	w.Start("req-1", resilience.RequeuePayload{RequestID: "req-1", Body: []byte("payload-1")})
	time.Sleep(10 * time.Millisecond)

	canceller := &fakeCanceller{}
	requeuer := &fakeRequeuer{}
	reaper := resilience.NewReaper(w, canceller, requeuer)

	recovered := reaper.Sweep(context.Background())

	if recovered != 1 {
		t.Errorf("recovered = %d, want 1", recovered)
	}
	if len(canceller.cancelled) != 1 || canceller.cancelled[0] != "req-1" {
		t.Errorf("cancelled = %v, want [req-1]", canceller.cancelled)
	}
	if len(requeuer.enqueued) != 1 || requeuer.enqueued[0] != "req-1" {
		t.Errorf("enqueued = %v, want [req-1]", requeuer.enqueued)
	}
}

func TestReaper_Sweep_HealthyRequestsAreLeftAlone(t *testing.T) {
	w := resilience.NewWatchdog(time.Hour)
	w.Start("req-1", resilience.RequeuePayload{RequestID: "req-1"})

	canceller := &fakeCanceller{}
	reaper := resilience.NewReaper(w, canceller, &fakeRequeuer{})

	if recovered := reaper.Sweep(context.Background()); recovered != 0 {
		t.Errorf("recovered = %d, want 0", recovered)
	}
	if len(canceller.cancelled) != 0 {
		t.Errorf("cancelled = %v, want none", canceller.cancelled)
	}
	if n := w.InFlightCount(); n != 1 {
		t.Errorf("InFlightCount() = %d, want 1 (still tracked, not stuck)", n)
	}
}

func TestReaper_Sweep_RequeueFailureDoesNotCountAsRecovered(t *testing.T) {
	w := resilience.NewWatchdog(time.Millisecond)
	w.Start("req-1", resilience.RequeuePayload{RequestID: "req-1"})
	time.Sleep(5 * time.Millisecond)

	reaper := resilience.NewReaper(w, &fakeCanceller{}, &fakeRequeuer{err: errors.New("redis unavailable")})

	if recovered := reaper.Sweep(context.Background()); recovered != 0 {
		t.Errorf("recovered = %d, want 0 when requeue fails", recovered)
	}
}

func TestReaper_Run_StopsOnContextCancel(t *testing.T) {
	w := resilience.NewWatchdog(time.Hour)
	reaper := resilience.NewReaper(w, &fakeCanceller{}, &fakeRequeuer{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		reaper.Run(ctx, time.Millisecond)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}
