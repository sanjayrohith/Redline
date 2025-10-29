ALTER TABLE inference_runs
    ADD COLUMN allocation_id TEXT,
    ADD COLUMN ttft_ms DOUBLE PRECISION,
    ADD COLUMN tpot_ms DOUBLE PRECISION,
    ADD COLUMN vram_peak_bytes BIGINT;

CREATE INDEX inference_runs_allocation_idx ON inference_runs (allocation_id);
