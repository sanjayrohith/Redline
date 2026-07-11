package db

import (
	"context"
	"time"
)

// APIKey is one row of the api_keys table. KeyHash never leaves this
// package's callers as anything other than an opaque stored hash.
type APIKey struct {
	ID            string
	UserID        string
	OrgID         *string
	KeyHash       string
	DisplayPrefix string
	Scopes        []string
	LastUsedAt    *time.Time
	RevokedAt     *time.Time
	CreatedAt     time.Time
}

// NewAPIKey is the set of fields required to persist a freshly generated key.
type NewAPIKey struct {
	UserID        string
	OrgID         *string
	KeyHash       string
	DisplayPrefix string
	Scopes        []string
}

// APIKeyRepository performs typed CRUD against the api_keys table.
type APIKeyRepository struct {
	pool *Pool
}

// NewAPIKeyRepository returns an APIKeyRepository bound to pool.
func NewAPIKeyRepository(pool *Pool) *APIKeyRepository {
	return &APIKeyRepository{pool: pool}
}

// Create persists a new API key record.
func (r *APIKeyRepository) Create(ctx context.Context, key NewAPIKey) (*APIKey, error) {
	var k APIKey
	err := r.pool.QueryRow(ctx,
		`INSERT INTO api_keys (user_id, org_id, key_hash, display_prefix, scopes)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id::text, user_id::text, org_id::text, key_hash, display_prefix,
		           scopes, last_used_at, revoked_at, created_at`,
		key.UserID, key.OrgID, key.KeyHash, key.DisplayPrefix, key.Scopes,
	).Scan(&k.ID, &k.UserID, &k.OrgID, &k.KeyHash, &k.DisplayPrefix,
		&k.Scopes, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &k, nil
}

// GetByPrefix returns the API key with the given display prefix, or ErrNotFound.
func (r *APIKeyRepository) GetByPrefix(ctx context.Context, prefix string) (*APIKey, error) {
	var k APIKey
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, user_id::text, org_id::text, key_hash, display_prefix,
		        scopes, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE display_prefix = $1`,
		prefix,
	).Scan(&k.ID, &k.UserID, &k.OrgID, &k.KeyHash, &k.DisplayPrefix,
		&k.Scopes, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &k, nil
}

// ListByUser returns every API key belonging to userID, newest first.
func (r *APIKeyRepository) ListByUser(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id::text, user_id::text, org_id::text, key_hash, display_prefix,
		        scopes, last_used_at, revoked_at, created_at
		 FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, mapError(err)
	}
	defer rows.Close()

	var keys []APIKey
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.UserID, &k.OrgID, &k.KeyHash, &k.DisplayPrefix,
			&k.Scopes, &k.LastUsedAt, &k.RevokedAt, &k.CreatedAt); err != nil {
			return nil, mapError(err)
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, mapError(err)
	}

	return keys, nil
}

// TouchLastUsed stamps the key's last-used time to now.
func (r *APIKeyRepository) TouchLastUsed(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET last_used_at = now() WHERE id = $1`,
		id,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllByUser revokes every currently-active key belonging to userID,
// returning how many were revoked. Used for immediate account suspension:
// unlike Revoke, revoking zero keys (a user with none active) is not an
// error.
func (r *APIKeyRepository) RevokeAllByUser(ctx context.Context, userID string) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`,
		userID,
	)
	if err != nil {
		return 0, mapError(err)
	}
	return tag.RowsAffected(), nil
}

// Revoke marks an active key revoked. It is a no-op error (ErrNotFound) if
// the key does not exist or is already revoked.
func (r *APIKeyRepository) Revoke(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
		id,
	)
	if err != nil {
		return mapError(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
