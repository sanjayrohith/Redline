package telemetry_test

import (
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/telemetry"
)

type fakeMetricsRecorder struct {
	ttftObserved []time.Duration
	tpotObserved []time.Duration
}

func (f *fakeMetricsRecorder) ObserveTimeToFirstToken(_, _ string, d time.Duration) {
	f.ttftObserved = append(f.ttftObserved, d)
}

func (f *fakeMetricsRecorder) ObserveInterTokenLatency(_, _ string, d time.Duration) {
	f.tpotObserved = append(f.tpotObserved, d)
}

func TestPublisher_ObserveTimeToFirstToken_RecordsAndPublishes(t *testing.T) {
	hub := telemetry.NewHub()
	samples, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	metrics := &fakeMetricsRecorder{}
	publisher := telemetry.NewPublisher(hub, metrics)

	publisher.ObserveTimeToFirstToken("llama-7b", "fp16", 42*time.Millisecond)

	if len(metrics.ttftObserved) != 1 || metrics.ttftObserved[0] != 42*time.Millisecond {
		t.Errorf("ttftObserved = %v, want [42ms]", metrics.ttftObserved)
	}

	select {
	case sample := <-samples:
		if sample.RunID != "llama-7b" {
			t.Errorf("RunID = %q, want llama-7b", sample.RunID)
		}
		if sample.TTFTMs == nil || *sample.TTFTMs != 42 {
			t.Errorf("TTFTMs = %v, want 42", sample.TTFTMs)
		}
		if sample.TPOTMs != nil {
			t.Errorf("TPOTMs = %v, want nil for a TTFT sample", sample.TPOTMs)
		}
	case <-time.After(time.Second):
		t.Fatal("hub never published the sample")
	}
}

func TestPublisher_ObserveInterTokenLatency_RecordsAndPublishes(t *testing.T) {
	hub := telemetry.NewHub()
	samples, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	metrics := &fakeMetricsRecorder{}
	publisher := telemetry.NewPublisher(hub, metrics)

	publisher.ObserveInterTokenLatency("llama-7b", "fp16", 8*time.Millisecond)

	if len(metrics.tpotObserved) != 1 || metrics.tpotObserved[0] != 8*time.Millisecond {
		t.Errorf("tpotObserved = %v, want [8ms]", metrics.tpotObserved)
	}

	select {
	case sample := <-samples:
		if sample.TPOTMs == nil || *sample.TPOTMs != 8 {
			t.Errorf("TPOTMs = %v, want 8", sample.TPOTMs)
		}
		if sample.TTFTMs != nil {
			t.Errorf("TTFTMs = %v, want nil for a TPOT sample", sample.TTFTMs)
		}
	case <-time.After(time.Second):
		t.Fatal("hub never published the sample")
	}
}
