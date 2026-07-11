package db

import (
	"context"
	"time"
)

// DeploymentStateTerminated marks a deployment permanently stopped, such
// as by account suspension - it will never be dispatched or resumed.
const DeploymentStateTerminated = "terminated"

// Deployment is one row of the deployments table.
type Deployment struct {
	ID           string
	ModelID      string
	UserID       *string
	AllocationID *string
	State        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	// GPUModel is the hardware profile ("A100", "H100", ...) this
	// deployment was placed onto, set once scheduling picks a node.
	// Telemetry rollups group by it alongside model_id, since the same
	// model's latency and throughput differ meaningfully by hardware.
	GPUModel *string
}

const deploymentColumns = `id::text, model_id::text, user_id::text, allocation_id, state, created_at, updated_at, gpu_model`

func scanDeployment(row interface{ Scan(...any) error }, d *Deployment) error {
	return row.Scan(&d.ID, &d.ModelID, &d.UserID, &d.AllocationID, &d.State, &d.CreatedAt, &d.UpdatedAt, &d.GPUModel)
}

// DeploymentRepository performs typed CRUD against the deployments table.
type DeploymentRepository struct {
	pool *Pool
}

// NewDeploymentRepository returns a DeploymentRepository bound to pool.
func NewDeploymentRepository(pool *Pool) *DeploymentRepository {
	return &DeploymentRepository{pool: pool}
}

// Create queues a new deployment for modelID, owned by userID (empty
// means unowned - a system or test-fixture deployment).
func (r *DeploymentRepository) Create(ctx context.Context, modelID, userID string) (*Deployment, error) {
	var ownerID *string
	if userID != "" {
		ownerID = &userID
	}

	var d Deployment
	err := scanDeployment(r.pool.QueryRow(ctx,
		`INSERT INTO deployments (model_id, user_id) VALUES ($1, $2) RETURNING `+deploymentColumns,
		modelID, ownerID,
	), &d)
	if err != nil {
		return nil, mapError(err)
	}
	return &d, nil
}

// GetByID returns the deployment with the given id, or ErrNotFound.
func (r *DeploymentRepository) GetByID(ctx context.Context, id string) (*Deployment, error) {
	var d Deployment
	err := scanDeployment(r.pool.QueryRow(ctx, `SELECT `+deploymentColumns+` FROM deployments WHERE id = $1`, id), &d)
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

// Terminate transitions a deployment to DeploymentStateTerminated. It is
// idempotent: terminating an already-terminated deployment is a no-op
// success, not ErrNotFound, since the caller's intent (this deployment
// must not be running) is already satisfied.
func (r *DeploymentRepository) Terminate(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE deployments SET state = $2, updated_at = now() WHERE id = $1 AND state != $2`,
		id, DeploymentStateTerminated,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetByID(ctx, id); err != nil {
			return err
		}
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

// SetGPUModel records the hardware profile a deployment was placed onto.
func (r *DeploymentRepository) SetGPUModel(ctx context.Context, id, gpuModel string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE deployments SET gpu_model = $2, updated_at = now() WHERE id = $1`,
		id, gpuModel,
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
		`SELECT `+deploymentColumns+` FROM deployments WHERE state = $1 ORDER BY created_at DESC`,
		state,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanDeployments(rows)
}

// ListActive returns every deployment not in a terminal state
// ("terminated" or "failed"), newest first - the set that should still
// have a live Nomad allocation backing it, if any.
func (r *DeploymentRepository) ListActive(ctx context.Context) ([]Deployment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+deploymentColumns+` FROM deployments WHERE state NOT IN ('terminated', 'failed') ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanDeployments(rows)
}

// ListByModel returns every deployment for modelID, newest first.
func (r *DeploymentRepository) ListByModel(ctx context.Context, modelID string) ([]Deployment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+deploymentColumns+` FROM deployments WHERE model_id = $1 ORDER BY created_at DESC`,
		modelID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanDeployments(rows)
}

// ListLiveByUser returns every non-terminal deployment owned by userID,
// newest first - what account suspension must terminate.
func (r *DeploymentRepository) ListLiveByUser(ctx context.Context, userID string) ([]Deployment, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+deploymentColumns+` FROM deployments
		 WHERE user_id = $1 AND state NOT IN ('terminated', 'failed') ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()
	return scanDeployments(rows)
}

func scanDeployments(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]Deployment, error) {
	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		if err := scanDeployment(rows, &d); err != nil {
			return nil, mapError(err)
		}
		deployments = append(deployments, d)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return deployments, nil
}
