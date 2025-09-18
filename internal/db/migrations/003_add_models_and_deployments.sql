CREATE TABLE models (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_url TEXT NOT NULL,
    revision TEXT NOT NULL,
    architecture TEXT NOT NULL,
    parameter_count BIGINT NOT NULL,
    dtype TEXT NOT NULL,
    vram_estimate_bytes BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT models_repo_revision_unique UNIQUE (repo_url, revision)
);

CREATE TABLE deployments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id UUID NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    allocation_id TEXT,
    state TEXT NOT NULL DEFAULT 'queued',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX deployments_model_id_idx ON deployments (model_id);
CREATE INDEX deployments_state_idx ON deployments (state);
