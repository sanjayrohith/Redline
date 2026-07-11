// Package policy enforces account-level terms-of-service and abuse
// policy: acceptance tracking, and immediate suspension that revokes
// every credential and stops every live workload belonging to the
// offending principal.
package policy

import (
	"context"
	"fmt"

	"github.com/sanjayrohith/redline/internal/db"
)

// UserSuspender marks a user account suspended. It is satisfied by
// *db.UserRepository.
type UserSuspender interface {
	Suspend(ctx context.Context, id string) error
}

// KeyRevoker revokes every active API key belonging to a user. It is
// satisfied by *db.APIKeyRepository.
type KeyRevoker interface {
	RevokeAllByUser(ctx context.Context, userID string) (revoked int64, err error)
}

// DeploymentLister lists a user's currently-live deployments. It is
// satisfied by *db.DeploymentRepository.
type DeploymentLister interface {
	ListLiveByUser(ctx context.Context, userID string) ([]db.Deployment, error)
}

// DeploymentTerminator marks a deployment terminated. It is satisfied by
// *db.DeploymentRepository.
type DeploymentTerminator interface {
	Terminate(ctx context.Context, id string) error
}

// AllocationStopper stops the Nomad job backing a deployment. It is
// satisfied by *nomadclient.Client.
type AllocationStopper interface {
	StopJob(ctx context.Context, jobID string, purge bool) (evalID string, err error)
}

// JobIDForDeployment maps a deployment id to its Nomad job id. It is
// satisfied by nomadclient.InferenceJobID.
type JobIDForDeployment func(deploymentID string) string

// Result summarizes one suspension's effect.
type Result struct {
	RevokedKeyCount         int64
	TerminatedDeploymentIDs []string
	// StopJobErrors holds deployments whose Nomad job failed to stop -
	// Nomad being unreachable does not abort the suspension (the account
	// is suspended and its keys revoked either way), but an operator
	// needs to know which allocations may still be running and require
	// manual cleanup once Nomad is reachable again.
	StopJobErrors map[string]error
}

// Enforcer suspends an account: marking it suspended, revoking every
// active API key, and terminating every live deployment it owns.
type Enforcer struct {
	users       UserSuspender
	keys        KeyRevoker
	deployments interface {
		DeploymentLister
		DeploymentTerminator
	}
	allocations AllocationStopper
	jobID       JobIDForDeployment
}

// NewEnforcer returns an Enforcer wired to its dependencies.
func NewEnforcer(
	users UserSuspender,
	keys KeyRevoker,
	deployments interface {
		DeploymentLister
		DeploymentTerminator
	},
	allocations AllocationStopper,
	jobID JobIDForDeployment,
) *Enforcer {
	return &Enforcer{users: users, keys: keys, deployments: deployments, allocations: allocations, jobID: jobID}
}

// Suspend immediately suspends userID: the account is marked suspended
// first (so even a failure in a later step still leaves the account
// unable to authenticate via a fresh login), then every active API key is
// revoked (blocking every API-key-authenticated request from the very
// next one onward - APIKeyAuth checks RevokedAt on each request), then
// every live deployment owned by userID is stopped and marked terminated.
func (e *Enforcer) Suspend(ctx context.Context, userID string) (*Result, error) {
	if err := e.users.Suspend(ctx, userID); err != nil {
		return nil, fmt.Errorf("policy: suspend user %s: %w", userID, err)
	}

	revoked, err := e.keys.RevokeAllByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("policy: revoke keys for %s: %w", userID, err)
	}

	result := &Result{RevokedKeyCount: revoked, StopJobErrors: make(map[string]error)}

	deployments, err := e.deployments.ListLiveByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("policy: list live deployments for %s: %w", userID, err)
	}

	for _, d := range deployments {
		if _, stopErr := e.allocations.StopJob(ctx, e.jobID(d.ID), true); stopErr != nil {
			result.StopJobErrors[d.ID] = stopErr
		}
		if err := e.deployments.Terminate(ctx, d.ID); err != nil {
			return result, fmt.Errorf("policy: mark deployment %s terminated: %w", d.ID, err)
		}
		result.TerminatedDeploymentIDs = append(result.TerminatedDeploymentIDs, d.ID)
	}

	return result, nil
}
