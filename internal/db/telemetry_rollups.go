package db

import (
	"context"
	"time"
)

// ModelRollup is one model+hardware-profile group's aggregated telemetry
// over a rollup window: the p50/p95/p99 latency distribution and overall
// throughput, computed directly from completed inference_runs rather
// than from telemetry_samples, since TTFT/TPOT are already denormalized
// onto the run at completion (see InferenceRunRepository.CompleteWithTelemetry).
type ModelRollup struct {
	ModelID  string
	GPUModel string // "unknown" when a deployment's gpu_model was never set.
	Window   time.Duration
	RunCount int64

	TTFTP50Ms *float64
	TTFTP95Ms *float64
	TTFTP99Ms *float64

	TPOTP50Ms *float64
	TPOTP95Ms *float64
	TPOTP99Ms *float64

	// ThroughputTokensPerSec is total completion tokens across the group
	// divided by total wall-clock run time - aggregate throughput, not
	// an average of per-run rates, since a handful of slow runs
	// shouldn't be weighted the same as many fast ones.
	ThroughputTokensPerSec *float64
	// TotalCostUSD sums every run's attributed cost (inference_runs.cost_usd)
	// in the group, for cost-per-model, cost-per-hardware-profile comparison.
	TotalCostUSD *float64
}

// TelemetryRollupRepository computes aggregate telemetry across
// completed inference runs.
type TelemetryRollupRepository struct {
	pool *Pool
}

// NewTelemetryRollupRepository returns a TelemetryRollupRepository bound to pool.
func NewTelemetryRollupRepository(pool *Pool) *TelemetryRollupRepository {
	return &TelemetryRollupRepository{pool: pool}
}

// ComputeModelRollups aggregates every inference run completed within
// window of now, grouped by model and the hardware profile (deployments.
// gpu_model) it ran on, computing the p50/p95/p99 TTFT and TPOT
// distribution and aggregate token throughput for each group.
func (r *TelemetryRollupRepository) ComputeModelRollups(ctx context.Context, window time.Duration) ([]ModelRollup, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT
		     ir.model_id::text,
		     COALESCE(d.gpu_model, 'unknown') AS gpu_model,
		     COUNT(*) AS run_count,
		     percentile_cont(0.5)  WITHIN GROUP (ORDER BY ir.ttft_ms) AS ttft_p50,
		     percentile_cont(0.95) WITHIN GROUP (ORDER BY ir.ttft_ms) AS ttft_p95,
		     percentile_cont(0.99) WITHIN GROUP (ORDER BY ir.ttft_ms) AS ttft_p99,
		     percentile_cont(0.5)  WITHIN GROUP (ORDER BY ir.tpot_ms) AS tpot_p50,
		     percentile_cont(0.95) WITHIN GROUP (ORDER BY ir.tpot_ms) AS tpot_p95,
		     percentile_cont(0.99) WITHIN GROUP (ORDER BY ir.tpot_ms) AS tpot_p99,
		     SUM(ir.completion_tokens) / NULLIF(EXTRACT(EPOCH FROM SUM(ir.completed_at - ir.started_at)), 0) AS throughput_tokens_per_sec,
		     SUM(ir.cost_usd) AS total_cost_usd
		 FROM inference_runs ir
		 JOIN deployments d ON d.id = ir.deployment_id
		 WHERE ir.completed_at IS NOT NULL
		   AND ir.started_at >= now() - make_interval(secs => $1)
		 GROUP BY ir.model_id, d.gpu_model`,
		window.Seconds(),
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var rollups []ModelRollup
	for rows.Next() {
		rollup := ModelRollup{Window: window}
		if err := rows.Scan(
			&rollup.ModelID, &rollup.GPUModel, &rollup.RunCount,
			&rollup.TTFTP50Ms, &rollup.TTFTP95Ms, &rollup.TTFTP99Ms,
			&rollup.TPOTP50Ms, &rollup.TPOTP95Ms, &rollup.TPOTP99Ms,
			&rollup.ThroughputTokensPerSec, &rollup.TotalCostUSD,
		); err != nil {
			return nil, mapError(err)
		}
		rollups = append(rollups, rollup)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return rollups, nil
}
