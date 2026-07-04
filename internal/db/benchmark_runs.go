package db

import (
	"context"
	"time"
)

// BenchmarkRun is one row of the benchmark_runs table: one standardized
// evaluation dispatched against a pinned deployment at an explicit
// sampling configuration.
type BenchmarkRun struct {
	ID           string
	DeploymentID string
	ModelID      string
	Precision    string
	Temperature  float64
	TopP         float64
	Seed         *int64
	TaskCount    int
	PassedCount  int
	Score        float64
	CreatedAt    time.Time
}

// NewBenchmarkRun is the set of fields required to record a completed run.
type NewBenchmarkRun struct {
	DeploymentID string
	ModelID      string
	Precision    string
	Temperature  float64
	TopP         float64
	Seed         *int64
	TaskCount    int
	PassedCount  int
	Score        float64
}

// BenchmarkRunRepository performs typed CRUD against the benchmark_runs table.
type BenchmarkRunRepository struct {
	pool *Pool
}

// NewBenchmarkRunRepository returns a BenchmarkRunRepository bound to pool.
func NewBenchmarkRunRepository(pool *Pool) *BenchmarkRunRepository {
	return &BenchmarkRunRepository{pool: pool}
}

const benchmarkRunColumns = `id::text, deployment_id::text, model_id::text, precision, temperature, top_p, seed,
	task_count, passed_count, score, created_at`

func scanBenchmarkRun(row interface{ Scan(...any) error }, r *BenchmarkRun) error {
	return row.Scan(&r.ID, &r.DeploymentID, &r.ModelID, &r.Precision, &r.Temperature, &r.TopP, &r.Seed,
		&r.TaskCount, &r.PassedCount, &r.Score, &r.CreatedAt)
}

// Create records a completed benchmark run.
func (r *BenchmarkRunRepository) Create(ctx context.Context, run NewBenchmarkRun) (*BenchmarkRun, error) {
	var out BenchmarkRun
	err := scanBenchmarkRun(r.pool.QueryRow(ctx,
		`INSERT INTO benchmark_runs (deployment_id, model_id, precision, temperature, top_p, seed, task_count, passed_count, score)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 RETURNING `+benchmarkRunColumns,
		run.DeploymentID, run.ModelID, run.Precision, run.Temperature, run.TopP, run.Seed,
		run.TaskCount, run.PassedCount, run.Score,
	), &out)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// ListByModel returns every benchmark run for modelID, newest first - the
// history a comparison view needs to plot score against precision and
// time.
func (r *BenchmarkRunRepository) ListByModel(ctx context.Context, modelID string) ([]BenchmarkRun, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+benchmarkRunColumns+` FROM benchmark_runs WHERE model_id = $1 ORDER BY created_at DESC`,
		modelID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var runs []BenchmarkRun
	for rows.Next() {
		var run BenchmarkRun
		if err := scanBenchmarkRun(rows, &run); err != nil {
			return nil, mapError(err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}
	return runs, nil
}
