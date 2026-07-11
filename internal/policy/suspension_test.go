package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/policy"
)

type fakeUserSuspender struct {
	suspended []string
	err       error
}

func (f *fakeUserSuspender) Suspend(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.suspended = append(f.suspended, id)
	return nil
}

type fakeKeyRevoker struct {
	revokedForUser []string
	count          int64
	err            error
}

func (f *fakeKeyRevoker) RevokeAllByUser(_ context.Context, userID string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.revokedForUser = append(f.revokedForUser, userID)
	return f.count, nil
}

type fakeDeploymentStore struct {
	live         []db.Deployment
	terminated   []string
	listErr      error
	terminateErr error
}

func (f *fakeDeploymentStore) ListLiveByUser(_ context.Context, _ string) ([]db.Deployment, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.live, nil
}

func (f *fakeDeploymentStore) Terminate(_ context.Context, id string) error {
	if f.terminateErr != nil {
		return f.terminateErr
	}
	f.terminated = append(f.terminated, id)
	return nil
}

type fakeAllocationStopper struct {
	stopped []string
	failFor map[string]error
}

func (f *fakeAllocationStopper) StopJob(_ context.Context, jobID string, _ bool) (string, error) {
	if err, ok := f.failFor[jobID]; ok {
		return "", err
	}
	f.stopped = append(f.stopped, jobID)
	return "eval-1", nil
}

func testJobID(deploymentID string) string { return "job-" + deploymentID }

func TestEnforcer_Suspend_RevokesKeysAndTerminatesLiveDeployments(t *testing.T) {
	users := &fakeUserSuspender{}
	keys := &fakeKeyRevoker{count: 3}
	deployments := &fakeDeploymentStore{live: []db.Deployment{{ID: "dep-1"}, {ID: "dep-2"}}}
	allocations := &fakeAllocationStopper{failFor: map[string]error{}}

	enforcer := policy.NewEnforcer(users, keys, deployments, allocations, testJobID)

	result, err := enforcer.Suspend(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}

	if len(users.suspended) != 1 || users.suspended[0] != "user-1" {
		t.Errorf("suspended = %v, want [user-1]", users.suspended)
	}
	if len(keys.revokedForUser) != 1 || keys.revokedForUser[0] != "user-1" {
		t.Errorf("revokedForUser = %v, want [user-1]", keys.revokedForUser)
	}
	if result.RevokedKeyCount != 3 {
		t.Errorf("RevokedKeyCount = %d, want 3", result.RevokedKeyCount)
	}
	if len(deployments.terminated) != 2 {
		t.Errorf("terminated = %v, want 2 deployments", deployments.terminated)
	}
	if len(allocations.stopped) != 2 {
		t.Errorf("stopped = %v, want 2 jobs", allocations.stopped)
	}
	if len(result.TerminatedDeploymentIDs) != 2 {
		t.Errorf("TerminatedDeploymentIDs = %v, want 2 entries", result.TerminatedDeploymentIDs)
	}
}

func TestEnforcer_Suspend_TerminatesDeploymentEvenWhenNomadStopFails(t *testing.T) {
	users := &fakeUserSuspender{}
	keys := &fakeKeyRevoker{}
	deployments := &fakeDeploymentStore{live: []db.Deployment{{ID: "dep-1"}}}
	allocations := &fakeAllocationStopper{failFor: map[string]error{"job-dep-1": errors.New("nomad unreachable")}}

	enforcer := policy.NewEnforcer(users, keys, deployments, allocations, testJobID)

	result, err := enforcer.Suspend(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Suspend() error = %v, want nil - a Nomad outage must not abort suspension", err)
	}
	if len(deployments.terminated) != 1 {
		t.Errorf("terminated = %v, want [dep-1] even though the Nomad stop failed", deployments.terminated)
	}
	if _, recorded := result.StopJobErrors["dep-1"]; !recorded {
		t.Error("StopJobErrors did not record the failed stop for dep-1")
	}
}

func TestEnforcer_Suspend_NoLiveDeploymentsIsFine(t *testing.T) {
	enforcer := policy.NewEnforcer(&fakeUserSuspender{}, &fakeKeyRevoker{}, &fakeDeploymentStore{}, &fakeAllocationStopper{}, testJobID)

	result, err := enforcer.Suspend(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("Suspend() error = %v", err)
	}
	if len(result.TerminatedDeploymentIDs) != 0 {
		t.Errorf("TerminatedDeploymentIDs = %v, want none", result.TerminatedDeploymentIDs)
	}
}

func TestEnforcer_Suspend_StopsEarlyIfUserSuspendFails(t *testing.T) {
	users := &fakeUserSuspender{err: errors.New("db unreachable")}
	keys := &fakeKeyRevoker{}

	enforcer := policy.NewEnforcer(users, keys, &fakeDeploymentStore{}, &fakeAllocationStopper{}, testJobID)

	if _, err := enforcer.Suspend(context.Background(), "user-1"); err == nil {
		t.Fatal("Suspend() error = nil, want an error when marking the user suspended fails")
	}
	if len(keys.revokedForUser) != 0 {
		t.Error("keys were revoked even though suspending the user failed first")
	}
}
