package scheduler

import (
	"context"
	"log/slog"

	"github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/db"
)

// liveAllocationStatuses are the Nomad ClientStatus values worth
// reconciling; a completed, failed, or lost allocation is already dead
// and consumes nothing to reclaim.
var liveAllocationStatuses = map[string]bool{"pending": true, "running": true}

// ActiveDeploymentLister lists every deployment not yet in a terminal
// state. It is satisfied by *db.DeploymentRepository.
type ActiveDeploymentLister interface {
	ListActive(ctx context.Context) ([]db.Deployment, error)
}

// OrphanOrchestrator is the Nomad surface OrphanReconciler needs. It is
// satisfied by nomadclient.Orchestrator.
type OrphanOrchestrator interface {
	ListAllocations(ctx context.Context) ([]*api.AllocationListStub, error)
	StopAllocation(ctx context.Context, allocID string) error
}

// OrphanReconciler finds live Nomad allocations for inference jobs that
// have no corresponding active deployment record - orphaned by a crash
// between provisioning and the record being written, or by a deployment
// row that was deleted out from under a still-running allocation - and
// stops them.
type OrphanReconciler struct {
	Orchestrator OrphanOrchestrator
	Deployments  ActiveDeploymentLister
	Logger       *slog.Logger
}

// NewOrphanReconciler returns an OrphanReconciler wired to orchestrator and deployments.
func NewOrphanReconciler(orchestrator OrphanOrchestrator, deployments ActiveDeploymentLister, logger *slog.Logger) *OrphanReconciler {
	return &OrphanReconciler{Orchestrator: orchestrator, Deployments: deployments, Logger: logger}
}

// Reconcile compares Nomad's live inference allocations against the
// database's active deployment records and stops every allocation with
// no matching record. Intended to run once at gateway startup, before
// the gateway starts accepting new scheduling decisions that might
// otherwise collide with a stale allocation. It returns the IDs of every
// allocation it stopped.
func (r *OrphanReconciler) Reconcile(ctx context.Context) ([]string, error) {
	allocs, err := r.Orchestrator.ListAllocations(ctx)
	if err != nil {
		return nil, err
	}

	active, err := r.Deployments.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	knownAllocationIDs := make(map[string]bool, len(active))
	for _, d := range active {
		if d.AllocationID != nil && *d.AllocationID != "" {
			knownAllocationIDs[*d.AllocationID] = true
		}
	}

	var stopped []string
	for _, alloc := range allocs {
		if _, ok := deploymentIDFromJobID(alloc.JobID); !ok {
			continue // not one of ours
		}
		if !liveAllocationStatuses[alloc.ClientStatus] {
			continue // already dead, nothing to reclaim
		}
		if knownAllocationIDs[alloc.ID] {
			continue // backed by an active deployment record
		}

		if err := r.Orchestrator.StopAllocation(ctx, alloc.ID); err != nil {
			r.Logger.Error("stop orphaned allocation", "allocation_id", alloc.ID, "job_id", alloc.JobID, "error", err)
			continue
		}

		r.Logger.Warn("reaped orphaned allocation with no active deployment record",
			"allocation_id", alloc.ID, "job_id", alloc.JobID)
		stopped = append(stopped, alloc.ID)
	}

	return stopped, nil
}
