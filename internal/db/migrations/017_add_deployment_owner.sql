ALTER TABLE deployments ADD COLUMN user_id UUID REFERENCES users (id) ON DELETE SET NULL;

CREATE INDEX deployments_user_idx ON deployments (user_id);
