// Package nomadclient wraps the Nomad Go SDK behind a narrow interface -
// job submission, allocation lookup, status streaming, and stop - so
// orchestration logic elsewhere in the gateway is mockable in tests.
package nomadclient

import (
	"context"
	"fmt"

	"github.com/hashicorp/nomad/api"
)

// Orchestrator is the subset of Nomad operations the scheduler depends
// on. It is satisfied by *Client; the interface exists so orchestration
// logic can be tested against a fake instead of a live Nomad cluster.
type Orchestrator interface {
	SubmitJob(ctx context.Context, job *api.Job) (evalID string, err error)
	SubmitJobIdempotent(ctx context.Context, job *api.Job) (evalID string, alreadyExisted bool, err error)
	Allocation(ctx context.Context, allocID string) (*api.Allocation, error)
	AllocationsForJob(ctx context.Context, jobID string) ([]*api.AllocationListStub, error)
	StreamEvents(ctx context.Context, topics map[api.Topic][]string, index uint64) (<-chan *api.Events, error)
	StopJob(ctx context.Context, jobID string, purge bool) (evalID string, err error)
}

// Client wraps a *api.Client bound to one Nomad cluster.
type Client struct {
	api *api.Client
}

var _ Orchestrator = (*Client)(nil)

// NewClient connects to the Nomad HTTP API at address (empty string uses
// the SDK's default, http://127.0.0.1:4646).
func NewClient(address string) (*Client, error) {
	cfg := api.DefaultConfig()
	if address != "" {
		cfg.Address = address
	}

	c, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("nomad: create client: %w", err)
	}
	return &Client{api: c}, nil
}

// SubmitJob registers job and returns the resulting evaluation ID.
func (c *Client) SubmitJob(ctx context.Context, job *api.Job) (string, error) {
	resp, _, err := c.api.Jobs().Register(job, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("nomad: register job: %w", err)
	}
	return resp.EvalID, nil
}

// Allocation returns full detail for one allocation.
func (c *Client) Allocation(ctx context.Context, allocID string) (*api.Allocation, error) {
	alloc, _, err := c.api.Allocations().Info(allocID, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("nomad: get allocation %s: %w", allocID, err)
	}
	return alloc, nil
}

// AllocationsForJob lists every allocation belonging to jobID.
func (c *Client) AllocationsForJob(ctx context.Context, jobID string) ([]*api.AllocationListStub, error) {
	allocs, _, err := c.api.Jobs().Allocations(jobID, false, (&api.QueryOptions{}).WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("nomad: list allocations for job %s: %w", jobID, err)
	}
	return allocs, nil
}

// StreamEvents streams allocation lifecycle events for topics, starting
// at index (0 for "from now").
func (c *Client) StreamEvents(ctx context.Context, topics map[api.Topic][]string, index uint64) (<-chan *api.Events, error) {
	ch, err := c.api.EventStream().Stream(ctx, topics, index, &api.QueryOptions{})
	if err != nil {
		return nil, fmt.Errorf("nomad: stream events: %w", err)
	}
	return ch, nil
}

// StopJob deregisters jobID, optionally purging its history, and returns
// the resulting evaluation ID.
func (c *Client) StopJob(ctx context.Context, jobID string, purge bool) (string, error) {
	evalID, _, err := c.api.Jobs().Deregister(jobID, purge, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("nomad: stop job %s: %w", jobID, err)
	}
	return evalID, nil
}
