package resilience_test

import (
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/resilience"
)

func TestWatchdog_TouchedRequestIsNotStuck(t *testing.T) {
	w := resilience.NewWatchdog(50 * time.Millisecond)
	w.Start("req-1", resilience.RequeuePayload{RequestID: "req-1"})

	time.Sleep(20 * time.Millisecond)
	w.Touch("req-1")

	if stuck := w.Stuck(time.Now().Add(30 * time.Millisecond)); len(stuck) != 0 {
		t.Errorf("Stuck() = %v, want none (touched within threshold)", stuck)
	}
}

func TestWatchdog_UntouchedRequestPastThresholdIsStuck(t *testing.T) {
	w := resilience.NewWatchdog(10 * time.Millisecond)
	w.Start("req-1", resilience.RequeuePayload{RequestID: "req-1", Model: "mock", Body: []byte(`{"model":"mock"}`)})

	stuck := w.Stuck(time.Now().Add(50 * time.Millisecond))
	if len(stuck) != 1 || stuck[0].RequestID != "req-1" {
		t.Fatalf("Stuck() = %v, want [req-1]", stuck)
	}
	if string(stuck[0].Body) != `{"model":"mock"}` {
		t.Errorf("Body = %s, want the original request payload", stuck[0].Body)
	}
}

func TestWatchdog_StuckRequestReportedOnlyOnce(t *testing.T) {
	w := resilience.NewWatchdog(10 * time.Millisecond)
	w.Start("req-1", resilience.RequeuePayload{RequestID: "req-1"})

	future := time.Now().Add(time.Hour)
	first := w.Stuck(future)
	second := w.Stuck(future)

	if len(first) != 1 {
		t.Fatalf("first Stuck() = %v, want one entry", first)
	}
	if len(second) != 0 {
		t.Errorf("second Stuck() = %v, want none (already reported and forgotten)", second)
	}
}

func TestWatchdog_ForgetRemovesFromTracking(t *testing.T) {
	w := resilience.NewWatchdog(time.Millisecond)
	w.Start("req-1", resilience.RequeuePayload{RequestID: "req-1"})
	w.Forget("req-1")

	if stuck := w.Stuck(time.Now().Add(time.Hour)); len(stuck) != 0 {
		t.Errorf("Stuck() = %v, want none (forgotten before the sweep)", stuck)
	}
	if n := w.InFlightCount(); n != 0 {
		t.Errorf("InFlightCount() = %d, want 0", n)
	}
}

func TestWatchdog_TouchOfUnknownRequestIsANoop(t *testing.T) {
	w := resilience.NewWatchdog(time.Millisecond)
	w.Touch("never-started") // must not panic
}
