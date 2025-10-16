package scheduler

import (
	"context"
	"errors"
	"fmt"

	"github.com/sanjayrohith/redline/internal/db"
)

// DeploymentState is one stage of a deployment's lifecycle.
type DeploymentState string

const (
	// StateQueued is a deployment waiting for scheduling.
	StateQueued DeploymentState = "queued"
	// StateProvisioning is a deployment whose Nomad allocation exists but
	// has not yet started running.
	StateProvisioning DeploymentState = "provisioning"
	// StateWarming is a deployment whose container is running but not
	// yet confirmed ready to serve traffic (e.g. still loading weights).
	StateWarming DeploymentState = "warming"
	// StateReady is a deployment serving traffic.
	StateReady DeploymentState = "ready"
	// StateDraining is a deployment finishing in-flight requests before
	// its allocation is torn down.
	StateDraining DeploymentState = "draining"
	// StateTerminated is a deployment cleanly shut down. Terminal.
	StateTerminated DeploymentState = "terminated"
	// StateFailed is a deployment that failed at any stage. Terminal.
	StateFailed DeploymentState = "failed"
)

// legalTransitions enumerates every state's allowed next states. A state
// not present in this map, or present with an empty slice, is terminal:
// no outgoing transition is legal (transitioning to itself is always a
// no-op exception handled separately by ValidateTransition).
var legalTransitions = map[DeploymentState][]DeploymentState{
	StateQueued:       {StateProvisioning, StateFailed},
	StateProvisioning: {StateWarming, StateReady, StateFailed},
	StateWarming:      {StateReady, StateFailed},
	StateReady:        {StateDraining, StateFailed},
	StateDraining:     {StateTerminated, StateFailed},
	StateTerminated:   nil,
	StateFailed:       nil,
}

// ErrIllegalTransition marks an attempted deployment state transition
// that the state machine does not permit.
var ErrIllegalTransition = errors.New("scheduler: illegal deployment state transition")

// ValidateTransition reports whether moving a deployment from from to to
// is legal. Transitioning to the same state is always legal (a no-op,
// tolerating a duplicate signal), even from a terminal state.
func ValidateTransition(from, to DeploymentState) error {
	if from == to {
		return nil
	}

	for _, allowed := range legalTransitions[from] {
		if allowed == to {
			return nil
		}
	}

	return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, from, to)
}

// IsTerminal reports whether state has no legal outgoing transition
// (other than the same-state no-op).
func IsTerminal(state DeploymentState) bool {
	return len(legalTransitions[state]) == 0
}

// DeploymentStateStore reads and writes a deployment's current lifecycle
// state and its backing Nomad allocation ID.
type DeploymentStateStore interface {
	CurrentState(ctx context.Context, id string) (DeploymentState, error)
	UpdateState(ctx context.Context, id string, state DeploymentState) error
	SetAllocationID(ctx context.Context, id, allocationID string) error
}

// DBDeploymentStore adapts *db.DeploymentRepository to DeploymentStateStore.
type DBDeploymentStore struct {
	Repo *db.DeploymentRepository
}

// CurrentState implements DeploymentStateStore.
func (s *DBDeploymentStore) CurrentState(ctx context.Context, id string) (DeploymentState, error) {
	d, err := s.Repo.GetByID(ctx, id)
	if err != nil {
		return "", err
	}
	return DeploymentState(d.State), nil
}

// UpdateState implements DeploymentStateStore.
func (s *DBDeploymentStore) UpdateState(ctx context.Context, id string, state DeploymentState) error {
	return s.Repo.UpdateState(ctx, id, string(state))
}

// SetAllocationID implements DeploymentStateStore.
func (s *DBDeploymentStore) SetAllocationID(ctx context.Context, id, allocationID string) error {
	return s.Repo.SetAllocationID(ctx, id, allocationID)
}
