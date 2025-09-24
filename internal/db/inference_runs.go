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
}

// InferenceRunRepository performs typed CRUD against the inference_runs table.
type InferenceRunRepository struct {
	pool *Pool
}

// NewInferenceRunRepository returns an InferenceRunRepository bound to pool.
func NewInferenceRunRepository(pool *Pool) *InferenceRunRepository {
	return &InferenceRunRepository{pool: pool}
}

// Create starts a new inference run record.
func (r *InferenceRunRepository) Create(ctx context.Context, deploymentID, modelID, userID string) (*InferenceRun, error) {
	var run InferenceRun
	err := r.pool.QueryRow(ctx,
		`INSERT INTO inference_runs (deployment_id, model_id, user_id) VALUES ($1, $2, $3)
		 RETURNING id::text, deployment_id::text, model_id::text, user_id::text,
		           started_at, completed_at, prompt_tokens, completion_tokens`,
		deploymentID, modelID, userID,
	).Scan(&run.ID, &run.DeploymentID, &run.ModelID, &run.UserID,
		&run.StartedAt, &run.CompletedAt, &run.PromptTokens, &run.CompletionTokens)
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
		        started_at, completed_at, prompt_tokens, completion_tokens
		 FROM inference_runs WHERE id = $1`,
		id,
	).Scan(&run.ID, &run.DeploymentID, &run.ModelID, &run.UserID,
		&run.StartedAt, &run.CompletedAt, &run.PromptTokens, &run.CompletionTokens)
	if err != nil {
		return nil, mapError(err)
	}
	return &run, nil
}

// Complete stamps a run as finished with its final token counts.
func (r *InferenceRunRepository) Complete(ctx context.Context, id string, promptTokens, completionTokens int) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE inference_runs
		 SET completed_at = now(), prompt_tokens = $2, completion_tokens = $3
		 WHERE id = $1`,
		id, promptTokens, completionTokens,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
