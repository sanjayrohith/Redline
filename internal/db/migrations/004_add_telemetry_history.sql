CREATE TABLE inference_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id UUID NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    model_id UUID NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX inference_runs_model_started_idx ON inference_runs (model_id, started_at);
CREATE INDEX inference_runs_user_started_idx ON inference_runs (user_id, started_at);

CREATE TABLE telemetry_samples (
    id BIGSERIAL PRIMARY KEY,
    inference_run_id UUID NOT NULL REFERENCES inference_runs (id) ON DELETE CASCADE,
    sampled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ttft_ms DOUBLE PRECISION,
    tpot_ms DOUBLE PRECISION,
    vram_bytes BIGINT
);

CREATE INDEX telemetry_samples_run_sampled_idx ON telemetry_samples (inference_run_id, sampled_at);
