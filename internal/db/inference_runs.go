package db

import (
	"context"
	"time"
)

// InferenceRun is one row of the inference_runs table.
type InferenceRun struct {
	ID               string
	DeploymentID     string
	ModelID          string
	UserID           string
	StartedAt        time.Time
	CompletedAt      *time.Time
	PromptTokens     int
	CompletionTokens int

	// AllocationID is the Nomad allocation this run actually executed on
	// - narrower than DeploymentID, which survives the deployment being
	// rescheduled onto a new allocation across its lifetime, while a run
	// only ever executes on the one allocation live when it was dispatched.
	AllocationID *string
	// TTFTMs and TPOTMs are this run's own admission-to-first-token and
	// average inter-token latency, denormalized onto the run itself at
	// completion time so a per-run lookup does not need to aggregate
	// telemetry_samples every time it is read. The underlying samples
	// remain in telemetry_samples for time-series inspection and for
	// TTFTMs/TPOTMs recomputation, if needed.
	TTFTMs *float64
	TPOTMs *float64
	// VRAMPeakBytes is the highest VRAM usage telemetry_samples observed
	// for this run.
	VRAMPeakBytes *int64
	// CostUSD attributes a concrete dollar cost to this run: allocation
	// wall time multiplied by the node's configured hourly rate (see
	// billing.ComputeCost), computed by the caller at completion time and
	// persisted here rather than recomputed on every read.
	CostUSD *float64
}

// InferenceRunRepository performs typed CRUD against the inference_runs table.
type InferenceRunRepository struct {
	pool *Pool
}

// NewInferenceRunRepository returns an InferenceRunRepository bound to pool.
func NewInferenceRunRepository(pool *Pool) *InferenceRunRepository {
	return &InferenceRunRepository{pool: pool}
}

// Create starts a new inference run record, bound to the allocation that
// will actually execute it.
func (r *InferenceRunRepository) Create(ctx context.Context, deploymentID, modelID, userID, allocationID string) (*InferenceRun, error) {
	var run InferenceRun
	err := r.pool.QueryRow(ctx,
		`INSERT INTO inference_runs (deployment_id, model_id, user_id, allocation_id) VALUES ($1, $2, $3, $4)
		 RETURNING id::text, deployment_id::text, model_id::text, user_id::text,
		           started_at, completed_at, prompt_tokens, completion_tokens,
		           allocation_id, ttft_ms, tpot_ms, vram_peak_bytes, cost_usd`,
		deploymentID, modelID, userID, allocationID,
	).Scan(&run.ID, &run.DeploymentID, &run.ModelID, &run.UserID,
		&run.StartedAt, &run.CompletedAt, &run.PromptTokens, &run.CompletionTokens,
		&run.AllocationID, &run.TTFTMs, &run.TPOTMs, &run.VRAMPeakBytes, &run.CostUSD)
	if err != nil {
		return nil, mapError(err)
	}
	return &run, nil
}

// GetByID returns the inference run with the given id, or ErrNotFound.
func (r *InferenceRunRepository) GetByID(ctx context.Context, id string) (*InferenceRun, error) {
	var run InferenceRun
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, deployment_id::text, model_id::text, user_id::text,
		        started_at, completed_at, prompt_tokens, completion_tokens,
		        allocation_id, ttft_ms, tpot_ms, vram_peak_bytes, cost_usd
		 FROM inference_runs WHERE id = $1`,
		id,
	).Scan(&run.ID, &run.DeploymentID, &run.ModelID, &run.UserID,
		&run.StartedAt, &run.CompletedAt, &run.PromptTokens, &run.CompletionTokens,
		&run.AllocationID, &run.TTFTMs, &run.TPOTMs, &run.VRAMPeakBytes, &run.CostUSD)
	if err != nil {
		return nil, mapError(err)
	}
	return &run, nil
}

// RunTelemetry is the durable per-run summary CompleteWithTelemetry
// writes: exactly what step 116's execution objective asks for -
// TTFT, TPOT, token counts, VRAM peak - stored alongside the run's
// already-recorded allocation identity for historical comparison.
type RunTelemetry struct {
	PromptTokens     int
	CompletionTokens int
	TTFTMs           *float64
	TPOTMs           *float64
	VRAMPeakBytes    *int64
	// CostUSD is the run's attributed dollar cost - see billing.ComputeCost.
	CostUSD *float64
}

// CompleteWithTelemetry stamps a run finished, writing its final token
// counts together with its telemetry summary in the same durable update -
// a run is never left half-recorded (tokens written, telemetry missing,
// or vice versa) by two separate writes racing a crash between them.
func (r *InferenceRunRepository) CompleteWithTelemetry(ctx context.Context, id string, t RunTelemetry) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE inference_runs
		 SET completed_at = now(), prompt_tokens = $2, completion_tokens = $3,
		     ttft_ms = $4, tpot_ms = $5, vram_peak_bytes = $6, cost_usd = $7
		 WHERE id = $1`,
		id, t.PromptTokens, t.CompletionTokens, t.TTFTMs, t.TPOTMs, t.VRAMPeakBytes, t.CostUSD,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
