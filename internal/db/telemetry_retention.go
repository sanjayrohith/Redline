package db

import (
	"context"
	"fmt"
	"time"
)

// TelemetryHourlyAggregate is one row of the telemetry_hourly_aggregates
// table: one model's downsampled telemetry for one hour bucket.
type TelemetryHourlyAggregate struct {
	ID           int64
	ModelID      string
	BucketHour   time.Time
	TTFTSumMs    float64
	TTFTCount    int64
	TPOTSumMs    float64
	TPOTCount    int64
	VRAMSumBytes float64
	VRAMCount    int64
	VRAMMaxBytes *int64
}

// AvgTTFTMs returns the bucket's mean TTFT, or nil if no sample in it
// carried a TTFT value.
func (a TelemetryHourlyAggregate) AvgTTFTMs() *float64 {
	return safeAvg(a.TTFTSumMs, a.TTFTCount)
}

// AvgTPOTMs returns the bucket's mean TPOT, or nil if no sample in it
// carried a TPOT value.
func (a TelemetryHourlyAggregate) AvgTPOTMs() *float64 {
	return safeAvg(a.TPOTSumMs, a.TPOTCount)
}

// AvgVRAMBytes returns the bucket's mean VRAM usage, or nil if no sample
// in it carried a VRAM reading.
func (a TelemetryHourlyAggregate) AvgVRAMBytes() *float64 {
	return safeAvg(a.VRAMSumBytes, a.VRAMCount)
}

func safeAvg(sum float64, count int64) *float64 {
	if count == 0 {
		return nil
	}
	avg := sum / float64(count)
	return &avg
}

// TelemetryRetentionJob downsamples raw telemetry_samples rows older
// than a configured threshold into telemetry_hourly_aggregates, then
// prunes them, keeping the raw samples table bounded regardless of how
// long historical rollups need to remain queryable.
type TelemetryRetentionJob struct {
	pool *Pool
}

// NewTelemetryRetentionJob returns a TelemetryRetentionJob bound to pool.
func NewTelemetryRetentionJob(pool *Pool) *TelemetryRetentionJob {
	return &TelemetryRetentionJob{pool: pool}
}

// Run downsamples every raw telemetry_samples row older than
// retentionThreshold into hourly aggregates - merged additively into
// any bucket a previous run already created, via sums and counts rather
// than pre-computed averages (see the aggregate table's own comment) -
// and deletes those raw rows, all within one transaction: a crash
// between downsampling and pruning can only ever leave raw rows
// un-pruned (safe to retry), never lose data that was pruned without
// first being durably aggregated.
func (j *TelemetryRetentionJob) Run(ctx context.Context, retentionThreshold time.Duration) (prunedSamples int64, err error) {
	tx, err := j.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("db: begin retention job: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO telemetry_hourly_aggregates
		     (model_id, bucket_hour, ttft_sum_ms, ttft_count, tpot_sum_ms, tpot_count, vram_sum_bytes, vram_count, vram_max_bytes)
		 SELECT
		     ir.model_id,
		     date_trunc('hour', ts.sampled_at),
		     COALESCE(SUM(ts.ttft_ms), 0), COUNT(ts.ttft_ms),
		     COALESCE(SUM(ts.tpot_ms), 0), COUNT(ts.tpot_ms),
		     COALESCE(SUM(ts.vram_bytes), 0), COUNT(ts.vram_bytes),
		     MAX(ts.vram_bytes)
		 FROM telemetry_samples ts
		 JOIN inference_runs ir ON ir.id = ts.inference_run_id
		 WHERE ts.sampled_at < now() - make_interval(secs => $1)
		 GROUP BY ir.model_id, date_trunc('hour', ts.sampled_at)
		 ON CONFLICT (model_id, bucket_hour) DO UPDATE SET
		     ttft_sum_ms    = telemetry_hourly_aggregates.ttft_sum_ms    + EXCLUDED.ttft_sum_ms,
		     ttft_count     = telemetry_hourly_aggregates.ttft_count     + EXCLUDED.ttft_count,
		     tpot_sum_ms    = telemetry_hourly_aggregates.tpot_sum_ms    + EXCLUDED.tpot_sum_ms,
		     tpot_count     = telemetry_hourly_aggregates.tpot_count     + EXCLUDED.tpot_count,
		     vram_sum_bytes = telemetry_hourly_aggregates.vram_sum_bytes + EXCLUDED.vram_sum_bytes,
		     vram_count     = telemetry_hourly_aggregates.vram_count     + EXCLUDED.vram_count,
		     vram_max_bytes = GREATEST(telemetry_hourly_aggregates.vram_max_bytes, EXCLUDED.vram_max_bytes)`,
		retentionThreshold.Seconds(),
	); err != nil {
		return 0, fmt.Errorf("db: downsample telemetry samples: %w", err)
	}

	tag, err := tx.Exec(ctx,
		`DELETE FROM telemetry_samples WHERE sampled_at < now() - make_interval(secs => $1)`,
		retentionThreshold.Seconds(),
	)
	if err != nil {
		return 0, fmt.Errorf("db: prune telemetry samples: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("db: commit retention job: %w", err)
	}

	return tag.RowsAffected(), nil
}

// ListHourlyAggregates returns every hourly aggregate bucket for
// modelID, oldest first.
func (j *TelemetryRetentionJob) ListHourlyAggregates(ctx context.Context, modelID string) ([]TelemetryHourlyAggregate, error) {
	rows, err := j.pool.Query(ctx,
		`SELECT id, model_id::text, bucket_hour, ttft_sum_ms, ttft_count, tpot_sum_ms, tpot_count,
		        vram_sum_bytes, vram_count, vram_max_bytes
		 FROM telemetry_hourly_aggregates WHERE model_id = $1 ORDER BY bucket_hour`,
		modelID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var aggregates []TelemetryHourlyAggregate
	for rows.Next() {
		var a TelemetryHourlyAggregate
		if err := rows.Scan(&a.ID, &a.ModelID, &a.BucketHour, &a.TTFTSumMs, &a.TTFTCount,
			&a.TPOTSumMs, &a.TPOTCount, &a.VRAMSumBytes, &a.VRAMCount, &a.VRAMMaxBytes); err != nil {
			return nil, mapError(err)
		}
		aggregates = append(aggregates, a)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return aggregates, nil
}
