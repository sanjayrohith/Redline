// Package telemetry streams live telemetry samples to subscribed
// frontend clients over WebSocket.
package telemetry

import (
	"sync"
	"time"
)

// Sample is one telemetry data point pushed to subscribers.
type Sample struct {
	RunID     string    `json:"run_id"`
	TTFTMs    *float64  `json:"ttft_ms,omitempty"`
	TPOTMs    *float64  `json:"tpot_ms,omitempty"`
	VRAMBytes *int64    `json:"vram_bytes,omitempty"`
	SampledAt time.Time `json:"sampled_at"`
}

// subscriberBufferSize bounds how many unread samples a single slow
// subscriber can accumulate before Publish starts dropping for it.
const subscriberBufferSize = 32

type subscriber struct {
	ch chan Sample
}

// Hub fans telemetry samples out to every currently-subscribed
// connection. It has no notion of the inference run or model a sample
// belongs to - filtering by that is the WebSocket handler's job at
// delivery time - the Hub's only responsibility is fan-out with
// per-connection backpressure.
type Hub struct {
	mu   sync.Mutex
	subs map[*subscriber]struct{}
}

// NewHub returns an empty Hub.
func NewHub() *Hub {
	return &Hub{subs: make(map[*subscriber]struct{})}
}

// Subscribe registers a new subscriber and returns its receive channel
// and the function to call when the subscriber disconnects. The
// returned channel is closed once unsubscribe is called - a caller
// ranging over it exits cleanly rather than blocking forever.
func (h *Hub) Subscribe() (<-chan Sample, func()) {
	sub := &subscriber{ch: make(chan Sample, subscriberBufferSize)}

	h.mu.Lock()
	h.subs[sub] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, sub)
			h.mu.Unlock()
			close(sub.ch)
		})
	}
	return sub.ch, unsubscribe
}

// Publish fans sample out to every current subscriber. A subscriber
// whose buffer is already full - a slow consumer that cannot keep up
// with the publish rate - has this sample dropped for it rather than
// blocking every other subscriber and the publisher itself waiting on
// it: backpressure here means the slow client falls behind and misses
// samples, not that live telemetry for every other client stalls too.
func (h *Hub) Publish(sample Sample) {
	h.mu.Lock()
	defer h.mu.Unlock()

	for sub := range h.subs {
		select {
		case sub.ch <- sample:
		default:
		}
	}
}

// SubscriberCount returns how many connections are currently subscribed.
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
