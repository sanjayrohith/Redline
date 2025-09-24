package db

import (
	"context"
	"time"
)

// TelemetrySample is one row of the telemetry_samples table.
type TelemetrySample struct {
	ID             int64
	InferenceRunID string
	SampledAt      time.Time
	TTFTMs         *float64
	TPOTMs         *float64
	VRAMBytes      *int64
}

// NewTelemetrySample is the set of fields required to record one sample.
type NewTelemetrySample struct {
	InferenceRunID string
	TTFTMs         *float64
	TPOTMs         *float64
	VRAMBytes      *int64
}

// TelemetrySampleRepository performs typed CRUD against the telemetry_samples table.
type TelemetrySampleRepository struct {
	pool *Pool
}

// NewTelemetrySampleRepository returns a TelemetrySampleRepository bound to pool.
func NewTelemetrySampleRepository(pool *Pool) *TelemetrySampleRepository {
	return &TelemetrySampleRepository{pool: pool}
}

// Create records one telemetry sample for an inference run.
func (r *TelemetrySampleRepository) Create(ctx context.Context, s NewTelemetrySample) (*TelemetrySample, error) {
	var out TelemetrySample
	err := r.pool.QueryRow(ctx,
		`INSERT INTO telemetry_samples (inference_run_id, ttft_ms, tpot_ms, vram_bytes)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, inference_run_id::text, sampled_at, ttft_ms, tpot_ms, vram_bytes`,
		s.InferenceRunID, s.TTFTMs, s.TPOTMs, s.VRAMBytes,
	).Scan(&out.ID, &out.InferenceRunID, &out.SampledAt, &out.TTFTMs, &out.TPOTMs, &out.VRAMBytes)
	if err != nil {
		return nil, mapError(err)
	}
	return &out, nil
}

// ListByRun returns every sample for runID, ordered by sample time.
func (r *TelemetrySampleRepository) ListByRun(ctx context.Context, runID string) ([]TelemetrySample, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, inference_run_id::text, sampled_at, ttft_ms, tpot_ms, vram_bytes
		 FROM telemetry_samples WHERE inference_run_id = $1 ORDER BY sampled_at`,
		runID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var samples []TelemetrySample
	for rows.Next() {
		var s TelemetrySample
		if err := rows.Scan(&s.ID, &s.InferenceRunID, &s.SampledAt, &s.TTFTMs, &s.TPOTMs, &s.VRAMBytes); err != nil {
			return nil, mapError(err)
		}
		samples = append(samples, s)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return samples, nil
}
