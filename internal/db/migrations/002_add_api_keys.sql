CREATE TABLE api_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    org_id UUID REFERENCES orgs (id) ON DELETE CASCADE,
    key_hash TEXT NOT NULL,
    display_prefix TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',
    last_used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT api_keys_display_prefix_unique UNIQUE (display_prefix)
);

CREATE INDEX api_keys_user_id_idx ON api_keys (user_id);
