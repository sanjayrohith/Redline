package db

import (
	"context"
	"time"
)

// Ingestion job states, in their expected lifecycle order (failure can
// follow any of them).
const (
	IngestionStateQueued      = "queued"
	IngestionStateDownloading = "downloading"
	IngestionStateVerifying   = "verifying"
	IngestionStateCached      = "cached"
	IngestionStateFailed      = "failed"
)

// IngestionJob is one row of the ingestion_jobs table: the durable
// progress record a frontend can poll to render a live ingestion bar.
type IngestionJob struct {
	ID              string
	RepoURL         string
	Revision        string
	State           string
	BytesTotal      int64
	BytesDownloaded int64
	ErrorMessage    *string
	ModelID         *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IngestionJobRepository performs typed CRUD against the ingestion_jobs table.
type IngestionJobRepository struct {
	pool *Pool
}

// NewIngestionJobRepository returns an IngestionJobRepository bound to pool.
func NewIngestionJobRepository(pool *Pool) *IngestionJobRepository {
	return &IngestionJobRepository{pool: pool}
}

const ingestionJobColumns = `id::text, repo_url, revision, state, bytes_total, bytes_downloaded,
	error_message, model_id::text, created_at, updated_at`

func scanIngestionJob(row interface{ Scan(...any) error }, j *IngestionJob) error {
	var modelID *string
	if err := row.Scan(&j.ID, &j.RepoURL, &j.Revision, &j.State, &j.BytesTotal, &j.BytesDownloaded,
		&j.ErrorMessage, &modelID, &j.CreatedAt, &j.UpdatedAt); err != nil {
		return err
	}
	j.ModelID = modelID
	return nil
}

// Create queues a new ingestion job for repoURL at revision.
func (r *IngestionJobRepository) Create(ctx context.Context, repoURL, revision string) (*IngestionJob, error) {
	var out IngestionJob
	err := scanIngestionJob(r.pool.QueryRow(ctx,
		`INSERT INTO ingestion_jobs (repo_url, revision) VALUES ($1, $2) RETURNING `+ingestionJobColumns,
		repoURL, revision,
	), &out)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// GetByID returns the ingestion job with the given id, or ErrNotFound.
func (r *IngestionJobRepository) GetByID(ctx context.Context, id string) (*IngestionJob, error) {
	var out IngestionJob
	err := scanIngestionJob(r.pool.QueryRow(ctx, `SELECT `+ingestionJobColumns+` FROM ingestion_jobs WHERE id = $1`, id), &out)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// UpdateState transitions the job to state.
func (r *IngestionJobRepository) UpdateState(ctx context.Context, id, state string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ingestion_jobs SET state = $2, updated_at = now() WHERE id = $1`,
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

// UpdateProgress records byte-level download progress, moving the job
// into IngestionStateDownloading if it is not already past that point.
func (r *IngestionJobRepository) UpdateProgress(ctx context.Context, id string, bytesTotal, bytesDownloaded int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ingestion_jobs
		 SET bytes_total = $2, bytes_downloaded = $3, state = $4, updated_at = now()
		 WHERE id = $1`,
		id, bytesTotal, bytesDownloaded, IngestionStateDownloading,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Complete marks the job cached and links it to the model it produced.
func (r *IngestionJobRepository) Complete(ctx context.Context, id, modelID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ingestion_jobs SET state = $2, model_id = $3, updated_at = now() WHERE id = $1`,
		id, IngestionStateCached, modelID,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Fail marks the job failed with the given error message.
func (r *IngestionJobRepository) Fail(ctx context.Context, id, errMsg string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ingestion_jobs SET state = $2, error_message = $3, updated_at = now() WHERE id = $1`,
		id, IngestionStateFailed, errMsg,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
