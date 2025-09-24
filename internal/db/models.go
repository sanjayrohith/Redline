package db

import (
	"context"
	"time"
)

// Model is one row of the models table.
type Model struct {
	ID                string
	RepoURL           string
	Revision          string
	Architecture      string
	ParameterCount    int64
	Dtype             string
	VRAMEstimateBytes int64
	CreatedAt         time.Time
}

// NewModel is the set of fields required to register an ingested model.
type NewModel struct {
	RepoURL           string
	Revision          string
	Architecture      string
	ParameterCount    int64
	Dtype             string
	VRAMEstimateBytes int64
}

// ModelRepository performs typed CRUD against the models table.
type ModelRepository struct {
	pool *Pool
}

// NewModelRepository returns a ModelRepository bound to pool.
func NewModelRepository(pool *Pool) *ModelRepository {
	return &ModelRepository{pool: pool}
}

// Create registers a newly ingested model.
func (r *ModelRepository) Create(ctx context.Context, m NewModel) (*Model, error) {
	var out Model
	err := r.pool.QueryRow(ctx,
		`INSERT INTO models (repo_url, revision, architecture, parameter_count, dtype, vram_estimate_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id::text, repo_url, revision, architecture, parameter_count, dtype, vram_estimate_bytes, created_at`,
		m.RepoURL, m.Revision, m.Architecture, m.ParameterCount, m.Dtype, m.VRAMEstimateBytes,
	).Scan(&out.ID, &out.RepoURL, &out.Revision, &out.Architecture, &out.ParameterCount, &out.Dtype, &out.VRAMEstimateBytes, &out.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// GetByID returns the model with the given id, or ErrNotFound.
func (r *ModelRepository) GetByID(ctx context.Context, id string) (*Model, error) {
	var out Model
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, repo_url, revision, architecture, parameter_count, dtype, vram_estimate_bytes, created_at
		 FROM models WHERE id = $1`,
		id,
	).Scan(&out.ID, &out.RepoURL, &out.Revision, &out.Architecture, &out.ParameterCount, &out.Dtype, &out.VRAMEstimateBytes, &out.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// GetByRepoRevision returns the model at the given repo URL and revision, or ErrNotFound.
func (r *ModelRepository) GetByRepoRevision(ctx context.Context, repoURL, revision string) (*Model, error) {
	var out Model
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, repo_url, revision, architecture, parameter_count, dtype, vram_estimate_bytes, created_at
		 FROM models WHERE repo_url = $1 AND revision = $2`,
		repoURL, revision,
	).Scan(&out.ID, &out.RepoURL, &out.Revision, &out.Architecture, &out.ParameterCount, &out.Dtype, &out.VRAMEstimateBytes, &out.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// List returns every model, newest first.
func (r *ModelRepository) List(ctx context.Context) ([]Model, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, repo_url, revision, architecture, parameter_count, dtype, vram_estimate_bytes, created_at
		 FROM models ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var models []Model
	for rows.Next() {
		var m Model
		if err := rows.Scan(&m.ID, &m.RepoURL, &m.Revision, &m.Architecture, &m.ParameterCount, &m.Dtype, &m.VRAMEstimateBytes, &m.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return models, nil
}
