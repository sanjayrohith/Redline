package nomadclient

import (
	"context"
	"fmt"

	"github.com/hashicorp/nomad/api"
)

// deadStatus is the Nomad job status meaning "stopped/purged" - a job in
// this state is safe to re-register under the same ID, since nothing live
// depends on it.
const deadStatus = "dead"

// SubmitJobIdempotent registers job only if no live job already exists
// under its ID. job.ID should be deterministic per deployment (see
// InferenceJobID), so a client retrying a submission after a network
// timeout - never having learned whether the first attempt actually
// reached Nomad - reconciles onto the existing job instead of registering
// a duplicate allocation.
//
// alreadyExisted reports whether a live job was found and re-registration
// was skipped; evalID is empty in that case, since no new evaluation was
// created.
func (c *Client) SubmitJobIdempotent(ctx context.Context, job *api.Job) (evalID string, alreadyExisted bool, err error) {
	if job.ID == nil || *job.ID == "" {
		return "", false, fmt.Errorf("nomad: job must have an ID for idempotent submission")
	}

	existing, _, infoErr := c.api.Jobs().Info(*job.ID, (&api.QueryOptions{}).WithContext(ctx))
	if infoErr == nil && existing != nil && (existing.Status == nil || *existing.Status != deadStatus) {
		return "", true, nil
	}

	evalID, err = c.SubmitJob(ctx, job)
	if err != nil {
		return "", false, err
	}
	return evalID, false, nil
}
