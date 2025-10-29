ALTER TABLE deployments ADD COLUMN gpu_model TEXT;

CREATE INDEX deployments_gpu_model_idx ON deployments (gpu_model);
