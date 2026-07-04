// Package resilience detects inference allocations that are ready but
// have stopped producing tokens, and recovers them: cancelling the stuck
// backend request so it stops holding GPU capacity, and requeuing the
// request it was serving so it gets a fresh attempt.
package resilience

import (
	"sync"
	"time"
)

// requestState tracks one in-flight request's token cadence and enough of
// its original request to requeue it if it goes stuck.
type requestState struct {
	lastTokenAt time.Time
	payload     RequeuePayload
}

// RequeuePayload is the durable information a stuck request needs to be
// dispatched again.
type RequeuePayload struct {
	RequestID string
	Model     string
	Body      []byte
}

// Watchdog tracks the last time each in-flight request produced a token.
// A request that goes longer than its configured threshold without one is
// "stuck": ready to generate (the allocation itself is healthy) but not
// actually making progress, which a plain liveness check on the
// allocation would never catch.
type Watchdog struct {
	mu        sync.Mutex
	requests  map[string]*requestState
	threshold time.Duration
}

// NewWatchdog returns a Watchdog that considers a request stuck once
// threshold has elapsed since its last token.
func NewWatchdog(threshold time.Duration) *Watchdog {
	return &Watchdog{requests: make(map[string]*requestState), threshold: threshold}
}

// Start begins tracking requestID, recording payload so a later Stuck
// detection can requeue it.
func (w *Watchdog) Start(requestID string, payload RequeuePayload) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.requests[requestID] = &requestState{lastTokenAt: time.Now(), payload: payload}
}

// Touch records that requestID just produced a token, resetting its
// stuck-detection clock.
func (w *Watchdog) Touch(requestID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if state, ok := w.requests[requestID]; ok {
		state.lastTokenAt = time.Now()
	}
}

// Forget stops tracking requestID - it finished, failed, or was
// cancelled through the normal request path, so it is no longer a
// candidate for stuck detection.
func (w *Watchdog) Forget(requestID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.requests, requestID)
}

// Stuck returns the payload of every request whose last token is older
// than the configured threshold as of now, and removes them from
// tracking - each returned payload is handed to the caller exactly once,
// so a slow-but-recovering sweep never reports (and re-recovers) the
// same request twice.
func (w *Watchdog) Stuck(now time.Time) []RequeuePayload {
	w.mu.Lock()
	defer w.mu.Unlock()

	var stuck []RequeuePayload
	for id, state := range w.requests {
		if now.Sub(state.lastTokenAt) >= w.threshold {
			stuck = append(stuck, state.payload)
			delete(w.requests, id)
		}
	}
	return stuck
}

// InFlightCount returns how many requests the Watchdog is currently
// tracking.
func (w *Watchdog) InFlightCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.requests)
}
