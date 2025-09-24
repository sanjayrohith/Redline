package db

import (
	"context"
	"time"
)

// RefreshToken is one row of the refresh_tokens table.
type RefreshToken struct {
	ID        string
	UserID    string
	TokenHash string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
}

// RefreshTokenRepository performs typed CRUD against the refresh_tokens table.
type RefreshTokenRepository struct {
	pool *Pool
}

// NewRefreshTokenRepository returns a RefreshTokenRepository bound to pool.
func NewRefreshTokenRepository(pool *Pool) *RefreshTokenRepository {
	return &RefreshTokenRepository{pool: pool}
}

// Create persists a new refresh token hash for userID.
func (r *RefreshTokenRepository) Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*RefreshToken, error) {
	var t RefreshToken
	err := r.pool.QueryRow(ctx,
		`INSERT INTO refresh_tokens (user_id, token_hash, expires_at) VALUES ($1, $2, $3)
		 RETURNING id::text, user_id::text, token_hash, expires_at, revoked_at, created_at`,
		userID, tokenHash, expiresAt,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &t, nil
}

// GetByHash returns the refresh token with the given hash, or ErrNotFound.
func (r *RefreshTokenRepository) GetByHash(ctx context.Context, tokenHash string) (*RefreshToken, error) {
	var t RefreshToken
	err := r.pool.QueryRow(ctx,
		`SELECT id::text, user_id::text, token_hash, expires_at, revoked_at, created_at
		 FROM refresh_tokens WHERE token_hash = $1`,
		tokenHash,
	).Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err != nil {
		return nil, mapError(err)
	}
	return &t, nil
}

// Revoke marks an active refresh token revoked. It reports ErrNotFound if
// the token does not exist or is already revoked.
func (r *RefreshTokenRepository) Revoke(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`,
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
