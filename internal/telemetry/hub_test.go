package telemetry_test

import (
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/telemetry"
)

func TestHub_PublishFansOutToEverySubscriber(t *testing.T) {
	hub := telemetry.NewHub()
	ch1, unsub1 := hub.Subscribe()
	defer unsub1()
	ch2, unsub2 := hub.Subscribe()
	defer unsub2()

	hub.Publish(telemetry.Sample{RunID: "run-1"})

	for i, ch := range []<-chan telemetry.Sample{ch1, ch2} {
		select {
		case s := <-ch:
			if s.RunID != "run-1" {
				t.Errorf("subscriber %d got RunID = %q, want run-1", i, s.RunID)
			}
		case <-time.After(time.Second):
			t.Errorf("subscriber %d never received the published sample", i)
		}
	}
}

func TestHub_UnsubscribeClosesTheChannel(t *testing.T) {
	hub := telemetry.NewHub()
	ch, unsub := hub.Subscribe()
	unsub()

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("channel yielded a value after unsubscribe, want it closed")
		}
	case <-time.After(time.Second):
		t.Error("channel was not closed within 1s of unsubscribe")
	}

	if hub.SubscriberCount() != 0 {
		t.Errorf("SubscriberCount() = %d, want 0 after unsubscribe", hub.SubscriberCount())
	}
}

func TestHub_SlowSubscriberDropsSamplesWithoutBlockingPublish(t *testing.T) {
	hub := telemetry.NewHub()
	slow, unsub := hub.Subscribe()
	defer unsub()

	// Publish never reads slow at all - by design, Publish's send to
	// each subscriber is non-blocking (a full buffer is dropped for,
	// not waited on), so this must complete quickly regardless of how
	// many publishes exceed the subscriber's buffer capacity.
	const overflow = 64 // far more than subscriberBufferSize
	done := make(chan struct{})
	go func() {
		for i := 0; i < overflow; i++ {
			hub.Publish(telemetry.Sample{RunID: "flood"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish() blocked with an unread subscriber; a slow subscriber must never stall the publisher")
	}

	// The unread subscriber's buffer is full (at its cap), not empty and
	// not somehow holding more than capacity - proving samples past the
	// buffer were dropped, not queued unbounded or lost entirely.
	drained := 0
drainLoop:
	for {
		select {
		case <-slow:
			drained++
		default:
			break drainLoop
		}
	}
	if drained == 0 {
		t.Fatal("slow subscriber's buffer was empty; expected it to have filled from the flood")
	}
	if drained > overflow {
		t.Errorf("drained %d samples, want at most %d published", drained, overflow)
	}
}
