// Package scheduler translates Nomad's allocation lifecycle into the
// gateway's own deployment records and, eventually, its scheduling
// decisions.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/hashicorp/nomad/api"

	"github.com/sanjayrohith/redline/internal/nomadclient"
)

// inferenceJobPrefix must match nomadclient.InferenceJobID's format, so a
// Nomad job ID can be reversed back into the deployment ID that produced it.
const inferenceJobPrefix = "inference-"

// StatusStreamer consumes the Nomad allocation event stream and persists
// each allocation's lifecycle transition to the matching deployment
// record, rejecting any transition the state machine (see state.go) does
// not permit rather than persisting it blindly.
type StatusStreamer struct {
	orchestrator nomadclient.Orchestrator
	deployments  DeploymentStateStore
	logger       *slog.Logger
}

// NewStatusStreamer returns a StatusStreamer wired to orchestrator and deployments.
func NewStatusStreamer(orchestrator nomadclient.Orchestrator, deployments DeploymentStateStore, logger *slog.Logger) *StatusStreamer {
	return &StatusStreamer{orchestrator: orchestrator, deployments: deployments, logger: logger}
}

// Run streams allocation events until ctx is cancelled or the stream
// closes, persisting a deployment state update for every allocation
// event whose job ID matches the inference job naming convention.
func (s *StatusStreamer) Run(ctx context.Context) error {
	topics := map[api.Topic][]string{api.TopicAllocation: {"*"}}

	events, err := s.orchestrator.StreamEvents(ctx, topics, 0)
	if err != nil {
		return fmt.Errorf("scheduler: start allocation event stream: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case batch, ok := <-events:
			if !ok {
				return nil
			}
			s.handleBatch(ctx, batch)
		}
	}
}

func (s *StatusStreamer) handleBatch(ctx context.Context, batch *api.Events) {
	if batch == nil {
		return
	}
	if batch.Err != nil {
		s.logger.Error("nomad event stream error", "error", batch.Err)
		return
	}

	for _, ev := range batch.Events {
		s.handleEvent(ctx, ev)
	}
}

func (s *StatusStreamer) handleEvent(ctx context.Context, ev api.Event) {
	alloc, err := ev.Allocation()
	if err != nil || alloc == nil {
		return
	}

	deploymentID, ok := deploymentIDFromJobID(alloc.JobID)
	if !ok {
		return // not one of ours
	}

	if err := s.deployments.SetAllocationID(ctx, deploymentID, alloc.ID); err != nil {
		s.logger.Error("record allocation id", "deployment_id", deploymentID, "allocation_id", alloc.ID, "error", err)
	}

	next := mapAllocationState(alloc.ClientStatus)
	if next == "" {
		return
	}

	current, err := s.deployments.CurrentState(ctx, deploymentID)
	if err != nil {
		s.logger.Error("read current deployment state", "deployment_id", deploymentID, "error", err)
		return
	}

	if err := ValidateTransition(current, next); err != nil {
		s.logger.Warn("dropped illegal deployment state transition",
			"deployment_id", deploymentID, "from", current, "to", next, "error", err)
		return
	}
	if current == next {
		return // no-op: nothing to persist
	}

	if err := s.deployments.UpdateState(ctx, deploymentID, next); err != nil {
		s.logger.Error("persist deployment state transition",
			"deployment_id", deploymentID, "state", next, "error", err)
	}
}

func deploymentIDFromJobID(jobID string) (string, bool) {
	deploymentID, ok := strings.CutPrefix(jobID, inferenceJobPrefix)
	if !ok || deploymentID == "" {
		return "", false
	}
	return deploymentID, true
}

// mapAllocationState translates a Nomad allocation ClientStatus into a
// DeploymentState. Empty means "no meaningful transition" (e.g. an
// intermediate status this gateway doesn't track).
func mapAllocationState(clientStatus string) DeploymentState {
	switch clientStatus {
	case "pending":
		return StateProvisioning
	case "running":
		return StateReady
	case "complete":
		return StateTerminated
	case "failed", "lost":
		return StateFailed
	default:
		return ""
	}
}
