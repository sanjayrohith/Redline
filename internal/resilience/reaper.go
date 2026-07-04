package resilience

import (
	"context"
	"time"
)

// Canceller stops an in-flight backend request. It is satisfied by
// inference.Backend's Cancel method.
type Canceller interface {
	Cancel(ctx context.Context, requestID string) error
}

// Requeuer durably re-dispatches a stuck request's payload for a fresh
// attempt. It is satisfied by *queue.Queue's Enqueue method bound to a
// retry queue.
type Requeuer interface {
	Enqueue(ctx context.Context, id, payload string) error
}

// Reaper periodically sweeps a Watchdog for stuck requests, cancelling
// each one's backend request - freeing the GPU capacity it was holding
// without making forward progress - and requeuing its payload for retry.
type Reaper struct {
	watchdog  *Watchdog
	canceller Canceller
	requeuer  Requeuer
}

// NewReaper returns a Reaper that recovers requests watchdog reports
// stuck by cancelling them through canceller and requeuing them through
// requeuer.
func NewReaper(watchdog *Watchdog, canceller Canceller, requeuer Requeuer) *Reaper {
	return &Reaper{watchdog: watchdog, canceller: canceller, requeuer: requeuer}
}

// Sweep runs one detection-and-recovery pass, returning the number of
// requests it recovered. A cancellation or requeue failure for one
// request does not stop the sweep from attempting the rest.
func (r *Reaper) Sweep(ctx context.Context) int {
	stuck := r.watchdog.Stuck(time.Now())
	recovered := 0
	for _, payload := range stuck {
		_ = r.canceller.Cancel(ctx, payload.RequestID)
		if err := r.requeuer.Enqueue(ctx, payload.RequestID, string(payload.Body)); err == nil {
			recovered++
		}
	}
	return recovered
}

// Run calls Sweep every interval until ctx is done.
func (r *Reaper) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Sweep(ctx)
		}
	}
}
