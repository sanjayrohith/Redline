CREATE TABLE benchmark_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    deployment_id UUID NOT NULL REFERENCES deployments (id) ON DELETE CASCADE,
    model_id UUID NOT NULL REFERENCES models (id) ON DELETE CASCADE,
    precision TEXT NOT NULL,
    temperature DOUBLE PRECISION NOT NULL,
    top_p DOUBLE PRECISION NOT NULL,
    seed BIGINT,
    task_count INT NOT NULL,
    passed_count INT NOT NULL,
    score DOUBLE PRECISION NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX benchmark_runs_model_idx ON benchmark_runs (model_id, created_at);
