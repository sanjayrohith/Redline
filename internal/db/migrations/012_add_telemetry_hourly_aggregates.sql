-- Sums and per-field counts are stored rather than pre-computed
-- averages: a bucket can receive additional raw samples across more
-- than one retention job run (new samples keep landing in the current
-- hour while older ones in that same hour already crossed the
-- retention threshold), and sums/counts merge correctly with a plain
-- addition on conflict, while two averages cannot be combined into a
-- correct combined average without also carrying their weights - which
-- is exactly what these counts are.
CREATE TABLE telemetry_hourly_aggregates (
    id BIGSERIAL PRIMARY KEY,
    model_id UUID NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    bucket_hour TIMESTAMPTZ NOT NULL,

    ttft_sum_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    ttft_count BIGINT NOT NULL DEFAULT 0,

    tpot_sum_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    tpot_count BIGINT NOT NULL DEFAULT 0,

    vram_sum_bytes DOUBLE PRECISION NOT NULL DEFAULT 0,
    vram_count BIGINT NOT NULL DEFAULT 0,
    vram_max_bytes BIGINT,

    CONSTRAINT telemetry_hourly_aggregates_unique UNIQUE (model_id, bucket_hour)
);

CREATE INDEX telemetry_hourly_aggregates_model_bucket_idx
    ON telemetry_hourly_aggregates (model_id, bucket_hour);
