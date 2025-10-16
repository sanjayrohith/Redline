package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/nomadclient"
)

// DefaultIdleThreshold is how long an allocation may sit idle before the
// reaper scales it to zero.
const DefaultIdleThreshold = 300 * time.Second

// ReadyDeploymentLister lists every deployment currently in a given
// state. It is satisfied by *db.DeploymentRepository.
type ReadyDeploymentLister interface {
	ListByState(ctx context.Context, state string) ([]db.Deployment, error)
}

// IdleDurationReader reports how long an allocation has sat idle. It is
// satisfied by *IdleTracker.
type IdleDurationReader interface {
	IdleDuration(ctx context.Context, allocationID string) (time.Duration, error)
}

// AllocationStopper gracefully stops one allocation. It is satisfied by
// nomadclient.Orchestrator.
type AllocationStopper interface {
	StopAllocation(ctx context.Context, allocID string) error
}

// IdleReaper terminates ready allocations that have sat idle beyond
// Threshold, reclaiming their GPU capacity for other work.
type IdleReaper struct {
	Deployments  ReadyDeploymentLister
	IdleTracker  IdleDurationReader
	Orchestrator AllocationStopper
	States       DeploymentStateStore
	Threshold    time.Duration
	Logger       *slog.Logger
}

// NewIdleReaper returns an IdleReaper with threshold (DefaultIdleThreshold
// if <= 0).
func NewIdleReaper(deployments ReadyDeploymentLister, idle IdleDurationReader, orchestrator AllocationStopper, states DeploymentStateStore, threshold time.Duration, logger *slog.Logger) *IdleReaper {
	if threshold <= 0 {
		threshold = DefaultIdleThreshold
	}
	return &IdleReaper{
		Deployments: deployments, IdleTracker: idle, Orchestrator: orchestrator,
		States: states, Threshold: threshold, Logger: logger,
	}
}

// ReapResult describes one allocation the reaper terminated.
type ReapResult struct {
	DeploymentID string
	AllocationID string
	IdleFor      time.Duration
	GPUHours     float64
}

// Reap scans every ready deployment, stops any whose allocation has sat
// idle beyond Threshold, transitions it to draining, and logs the
// reclaimed GPU-hours (the allocation's total lifetime, since that
// capacity is now free for other work). It returns every allocation it reaped.
func (r *IdleReaper) Reap(ctx context.Context) ([]ReapResult, error) {
	ready, err := r.Deployments.ListByState(ctx, string(StateReady))
	if err != nil {
		return nil, err
	}

	var results []ReapResult
	for _, d := range ready {
		if d.AllocationID == nil || *d.AllocationID == "" {
			continue
		}

		idleFor, err := r.IdleTracker.IdleDuration(ctx, *d.AllocationID)
		if err != nil {
			if err != ErrNoIdleRecord {
				r.Logger.Error("read idle duration", "deployment_id", d.ID, "allocation_id", *d.AllocationID, "error", err)
			}
			continue
		}
		if idleFor < r.Threshold {
			continue
		}

		if err := r.Orchestrator.StopAllocation(ctx, *d.AllocationID); err != nil {
			r.Logger.Error("stop idle allocation", "deployment_id", d.ID, "allocation_id", *d.AllocationID, "error", err)
			continue
		}

		if err := ValidateTransition(StateReady, StateDraining); err == nil {
			if err := r.States.UpdateState(ctx, d.ID, StateDraining); err != nil {
				r.Logger.Error("persist draining state", "deployment_id", d.ID, "error", err)
			}
		}

		gpuHours := time.Since(d.CreatedAt).Hours()
		r.Logger.Info("reaped idle allocation",
			"deployment_id", d.ID, "allocation_id", *d.AllocationID,
			"idle_for", idleFor, "gpu_hours_reclaimed", gpuHours)

		results = append(results, ReapResult{
			DeploymentID: d.ID, AllocationID: *d.AllocationID, IdleFor: idleFor, GPUHours: gpuHours,
		})
	}

	return results, nil
}

var _ AllocationStopper = (nomadclient.Orchestrator)(nil)
