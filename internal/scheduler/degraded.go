// Package scheduler already owns placement, allocation, and retry
// policy; this file adds the piece specific to Nomad itself going
// unreachable, as distinct from a single placement failing.
package scheduler

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/hashicorp/nomad/api"
)

// JobSubmitter is the Nomad-facing dependency DegradedModeDispatcher
// needs. It is satisfied by *nomadclient.Client.
type JobSubmitter interface {
	SubmitJobIdempotent(ctx context.Context, job *api.Job) (evalID string, alreadyExisted bool, err error)
}

// DeploymentQueue durably records a deployment that could not be
// dispatched, for a later retry once Nomad is reachable again. It is
// satisfied by *queue.Queue's Enqueue method bound to a retry queue.
type DeploymentQueue interface {
	Enqueue(ctx context.Context, id, payload string) error
}

// DegradedModeDispatcher submits new deployments to Nomad when it is
// reachable, and queues them for later retry rather than failing the
// request when it is not. Existing ready allocations are unaffected by
// either path: the inference request handlers never call into Nomad at
// request time, only at deployment placement, so a Nomad outage only
// ever blocks *new* deployments from launching - it never interrupts a
// request already being served by an allocation that launched earlier.
type DegradedModeDispatcher struct {
	orchestrator JobSubmitter
	retryQueue   DeploymentQueue
	degraded     atomic.Bool
}

// NewDegradedModeDispatcher returns a DegradedModeDispatcher that submits
// through orchestrator and queues failed submissions through retryQueue.
func NewDegradedModeDispatcher(orchestrator JobSubmitter, retryQueue DeploymentQueue) *DegradedModeDispatcher {
	return &DegradedModeDispatcher{orchestrator: orchestrator, retryQueue: retryQueue}
}

// Dispatch submits job for deploymentID. If Nomad accepts it, evalID is
// the resulting evaluation and queued is false. If submission fails for
// any reason - Nomad unreachable, an internal Nomad error - the
// deployment is durably queued for retry instead of the failure
// propagating to the caller, and queued is true with a nil error: from
// the caller's perspective, "the deployment was accepted, dispatch is
// pending" is the correct outcome even during an outage.
func (d *DegradedModeDispatcher) Dispatch(ctx context.Context, deploymentID string, job *api.Job) (evalID string, queued bool, err error) {
	evalID, _, submitErr := d.orchestrator.SubmitJobIdempotent(ctx, job)
	if submitErr == nil {
		d.degraded.Store(false)
		return evalID, false, nil
	}

	d.degraded.Store(true)
	if queueErr := d.retryQueue.Enqueue(ctx, deploymentID, deploymentID); queueErr != nil {
		return "", false, fmt.Errorf("scheduler: nomad unreachable (%w) and failed to queue for retry: %w", submitErr, queueErr)
	}
	return "", true, nil
}

// Degraded reports whether the most recent Dispatch attempt (across every
// deployment) had to fall back to queuing rather than reaching Nomad -
// the signal a dashboard or /readyz surfaces as a degraded, not failed,
// state.
func (d *DegradedModeDispatcher) Degraded() bool {
	return d.degraded.Load()
}
