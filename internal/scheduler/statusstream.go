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

// DeploymentStateUpdater persists a deployment's state transition. It is
// satisfied by *db.DeploymentRepository.
type DeploymentStateUpdater interface {
	UpdateState(ctx context.Context, id, state string) error
}

// inferenceJobPrefix must match nomadclient.InferenceJobID's format, so a
// Nomad job ID can be reversed back into the deployment ID that produced it.
const inferenceJobPrefix = "inference-"

// StatusStreamer consumes the Nomad allocation event stream and persists
// each allocation's lifecycle transition to the matching deployment
// record. The state names used here are provisional; the authoritative
// deployment state machine and its legal transitions are defined
// alongside it, layered on top of this stream rather than replacing it.
type StatusStreamer struct {
	orchestrator nomadclient.Orchestrator
	deployments  DeploymentStateUpdater
	logger       *slog.Logger
}

// NewStatusStreamer returns a StatusStreamer wired to orchestrator and deployments.
func NewStatusStreamer(orchestrator nomadclient.Orchestrator, deployments DeploymentStateUpdater, logger *slog.Logger) *StatusStreamer {
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

	state := mapAllocationState(alloc.ClientStatus)
	if state == "" {
		return
	}

	if err := s.deployments.UpdateState(ctx, deploymentID, state); err != nil {
		s.logger.Error("persist deployment state transition",
			"deployment_id", deploymentID, "state", state, "error", err)
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
// deployment state string. Empty means "no meaningful transition" (e.g.
// an intermediate status this gateway doesn't track).
func mapAllocationState(clientStatus string) string {
	switch clientStatus {
	case "pending":
		return "provisioning"
	case "running":
		return "ready"
	case "complete":
		return "terminated"
	case "failed", "lost":
		return "failed"
	default:
		return ""
	}
}
