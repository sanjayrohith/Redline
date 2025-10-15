package scheduler

import (
	"context"
	"fmt"

	"github.com/sanjayrohith/redline/internal/db"
)

// WarmPoolFinder lists every deployment for a model. It is satisfied by
// *db.DeploymentRepository.
type WarmPoolFinder interface {
	ListByModel(ctx context.Context, modelID string) ([]db.Deployment, error)
}

// FindWarmDeployment looks for an existing ready allocation already
// serving modelID, so a request can route to it directly instead of
// provisioning a fresh one - collapsing the common case to zero
// cold-start latency. It returns (nil, nil), not an error, when no warm
// deployment exists: that is the expected outcome that tells the caller
// to cold-start.
func FindWarmDeployment(ctx context.Context, finder WarmPoolFinder, modelID string) (*db.Deployment, error) {
	deployments, err := finder.ListByModel(ctx, modelID)
	if err != nil {
		return nil, fmt.Errorf("scheduler: list deployments for model %s: %w", modelID, err)
	}

	for i := range deployments {
		if deployments[i].State == string(StateReady) {
			return &deployments[i], nil
		}
	}

	return nil, nil
}
