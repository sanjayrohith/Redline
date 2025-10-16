package db

import (
	"context"
	"time"
)

// Deployment is one row of the deployments table.
type Deployment struct {
	ID           string
	ModelID      string
	AllocationID *string
	State        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DeploymentRepository performs typed CRUD against the deployments table.
type DeploymentRepository struct {
	pool *Pool
}

// NewDeploymentRepository returns a DeploymentRepository bound to pool.
func NewDeploymentRepository(pool *Pool) *DeploymentRepository {
	return &DeploymentRepository{pool: pool}
}

// Create queues a new deployment for modelID.
func (r *DeploymentRepository) Create(ctx context.Context, modelID string) (*Deployment, error) {
	var d Deployment
	err := r.pool.QueryRow(ctx,
		`INSERT INTO deployments (model_id) VALUES ($1)
		 RETURNING id::text, model_id::text, allocation_id, state, created_at, updated_at`,
		modelID,
	).Scan(&d.ID, &d.ModelID, &d.AllocationID, &d.State, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &d, nil
}

// GetByID returns the deployment with the given id, or ErrNotFound.
func (r *DeploymentRepository) GetByID(ctx context.Context, id string) (*Deployment, error) {
	var d Deployment
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, model_id::text, allocation_id, state, created_at, updated_at
		 FROM deployments WHERE id = $1`,
		id,
	).Scan(&d.ID, &d.ModelID, &d.AllocationID, &d.State, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &d, nil
}

// UpdateState transitions a deployment to state and stamps updated_at.
func (r *DeploymentRepository) UpdateState(ctx context.Context, id, state string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE deployments SET state = $2, updated_at = now() WHERE id = $1`,
		id, state,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetAllocationID records the Nomad allocation backing a deployment, once known.
func (r *DeploymentRepository) SetAllocationID(ctx context.Context, id, allocationID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE deployments SET allocation_id = $2, updated_at = now() WHERE id = $1`,
		id, allocationID,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListByState returns every deployment currently in state, newest first.
func (r *DeploymentRepository) ListByState(ctx context.Context, state string) ([]Deployment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, model_id::text, allocation_id, state, created_at, updated_at
		 FROM deployments WHERE state = $1 ORDER BY created_at DESC`,
		state,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.ModelID, &d.AllocationID, &d.State, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, mapError(err)
		}
		deployments = append(deployments, d)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return deployments, nil
}

// ListByModel returns every deployment for modelID, newest first.
func (r *DeploymentRepository) ListByModel(ctx context.Context, modelID string) ([]Deployment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, model_id::text, allocation_id, state, created_at, updated_at
		 FROM deployments WHERE model_id = $1 ORDER BY created_at DESC`,
		modelID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.ModelID, &d.AllocationID, &d.State, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, mapError(err)
		}
		deployments = append(deployments, d)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return deployments, nil
}
