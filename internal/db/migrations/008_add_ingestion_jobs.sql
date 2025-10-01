CREATE TABLE ingestion_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    repo_url TEXT NOT NULL,
    revision TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'queued',
    bytes_total BIGINT NOT NULL DEFAULT 0,
    bytes_downloaded BIGINT NOT NULL DEFAULT 0,
    error_message TEXT,
    model_id UUID REFERENCES models (id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX ingestion_jobs_state_idx ON ingestion_jobs (state);
CREATE INDEX ingestion_jobs_repo_revision_idx ON ingestion_jobs (repo_url, revision);
