package db

import (
	"context"
	"time"
)

// Model is one row of the models table. VRAM*Bytes fields are each the
// full total (weights plus KV cache) for that precision variant;
// KVCacheBytes is the shared KV-cache component of all three.
type Model struct {
	ID                    string
	RepoURL               string
	Revision              string
	Architecture          string
	ParameterCount        int64
	Dtype                 string
	VRAMEstimateFP16Bytes int64
	VRAMEstimateFP8Bytes  int64
	VRAMEstimateInt4Bytes int64
	KVCacheBytes          int64
	CreatedAt             time.Time
}

// NewModel is the set of fields required to register an ingested model.
type NewModel struct {
	RepoURL               string
	Revision              string
	Architecture          string
	ParameterCount        int64
	Dtype                 string
	VRAMEstimateFP16Bytes int64
	VRAMEstimateFP8Bytes  int64
	VRAMEstimateInt4Bytes int64
	KVCacheBytes          int64
}

// ModelRepository performs typed CRUD against the models table.
type ModelRepository struct {
	pool *Pool
}

// NewModelRepository returns a ModelRepository bound to pool.
func NewModelRepository(pool *Pool) *ModelRepository {
	return &ModelRepository{pool: pool}
}

const modelColumns = `id::text, repo_url, revision, architecture, parameter_count, dtype,
	vram_estimate_bytes, vram_fp8_bytes, vram_int4_bytes, kv_cache_bytes, created_at`

func scanModel(row interface{ Scan(...any) error }, m *Model) error {
	return row.Scan(&m.ID, &m.RepoURL, &m.Revision, &m.Architecture, &m.ParameterCount, &m.Dtype,
		&m.VRAMEstimateFP16Bytes, &m.VRAMEstimateFP8Bytes, &m.VRAMEstimateInt4Bytes, &m.KVCacheBytes, &m.CreatedAt)
}

// Create registers a newly ingested model.
func (r *ModelRepository) Create(ctx context.Context, m NewModel) (*Model, error) {
	var out Model
	err := scanModel(r.pool.QueryRow(ctx,
		`INSERT INTO models (repo_url, revision, architecture, parameter_count, dtype,
		                     vram_estimate_bytes, vram_fp8_bytes, vram_int4_bytes, kv_cache_bytes)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+modelColumns,
		m.RepoURL, m.Revision, m.Architecture, m.ParameterCount, m.Dtype,
		m.VRAMEstimateFP16Bytes, m.VRAMEstimateFP8Bytes, m.VRAMEstimateInt4Bytes, m.KVCacheBytes,
	), &out)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// GetByID returns the model with the given id, or ErrNotFound.
func (r *ModelRepository) GetByID(ctx context.Context, id string) (*Model, error) {
	var out Model
	err := scanModel(r.pool.QueryRow(ctx, `SELECT `+modelColumns+` FROM models WHERE id = $1`, id), &out)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// GetByRepoRevision returns the model at the given repo URL and revision, or ErrNotFound.
func (r *ModelRepository) GetByRepoRevision(ctx context.Context, repoURL, revision string) (*Model, error) {
	var out Model
	err := scanModel(r.pool.QueryRow(ctx,
		`SELECT `+modelColumns+` FROM models WHERE repo_url = $1 AND revision = $2`,
		repoURL, revision,
	), &out)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// List returns every model, newest first.
func (r *ModelRepository) List(ctx context.Context) ([]Model, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+modelColumns+` FROM models ORDER BY created_at DESC`)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var models []Model
	for rows.Next() {
		var m Model
		if err := scanModel(rows, &m); err != nil {
			return nil, mapError(err)
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return models, nil
}
