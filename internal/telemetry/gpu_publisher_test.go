package telemetry_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/telemetry"
)

func TestPublishGPUSamples_PublishesOnePerDevice(t *testing.T) {
	hub := telemetry.NewHub()
	samples, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	sample := func(context.Context) ([]telemetry.GPUSample, error) {
		return []telemetry.GPUSample{
			{DeviceIndex: "0", UtilizationPercent: 42, VRAMUsedBytes: 1000, VRAMTotalBytes: 8000},
			{DeviceIndex: "1", UtilizationPercent: 7, VRAMUsedBytes: 200, VRAMTotalBytes: 8000},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go telemetry.PublishGPUSamples(ctx, hub, sample, 5*time.Millisecond)

	seen := map[string]bool{}
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case s := <-samples:
			seen[s.RunID] = true
			if s.GPUUtilizationPct == nil || s.VRAMBytes == nil || s.VRAMTotalBytes == nil {
				t.Fatalf("sample %+v missing a GPU field", s)
			}
		case <-deadline:
			t.Fatalf("timed out waiting for both devices, saw %v", seen)
		}
	}

	if !seen["gpu:0"] || !seen["gpu:1"] {
		t.Errorf("seen = %v, want gpu:0 and gpu:1", seen)
	}
}

func TestPublishGPUSamples_StopsOnContextCancel(t *testing.T) {
	hub := telemetry.NewHub()
	calls := 0
	sample := func(context.Context) ([]telemetry.GPUSample, error) {
		calls++
		return nil, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		telemetry.PublishGPUSamples(ctx, hub, sample, time.Millisecond)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("PublishGPUSamples did not return after context cancellation")
	}
}

func TestPublishGPUSamples_SwallowsPollingErrors(t *testing.T) {
	hub := telemetry.NewHub()
	samples, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	attempt := 0
	sample := func(context.Context) ([]telemetry.GPUSample, error) {
		attempt++
		if attempt == 1 {
			return nil, errors.New("nvidia-smi: no devices found")
		}
		return []telemetry.GPUSample{{DeviceIndex: "0"}}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go telemetry.PublishGPUSamples(ctx, hub, sample, 5*time.Millisecond)

	select {
	case s := <-samples:
		if s.RunID != "gpu:0" {
			t.Errorf("RunID = %q, want gpu:0", s.RunID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("publisher never recovered from a polling error")
	}
}
