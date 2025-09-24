package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sanjayrohith/redline/internal/db"
)

// SessionConfig configures access and refresh token issuance.
type SessionConfig struct {
	SigningKey      []byte
	Issuer          string
	Audience        string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// AccessClaims is the JWT claim set carried by a gateway access token.
type AccessClaims struct {
	jwt.RegisteredClaims
}

// RefreshTokenStore is the persistence dependency IssueRefreshToken needs.
// It is satisfied by *db.RefreshTokenRepository; the interface exists so
// SessionIssuer is testable without a real database.
type RefreshTokenStore interface {
	Create(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (*db.RefreshToken, error)
	GetByHash(ctx context.Context, tokenHash string) (*db.RefreshToken, error)
	Revoke(ctx context.Context, id string) error
}

// Session is the pair of tokens handed to a newly authenticated browser.
type Session struct {
	AccessToken           string
	AccessTokenExpiresAt  time.Time
	RefreshToken          string
	RefreshTokenExpiresAt time.Time
}

// SessionIssuer issues and rotates browser session tokens.
type SessionIssuer struct {
	cfg           SessionConfig
	refreshTokens RefreshTokenStore
}

// NewSessionIssuer returns a SessionIssuer bound to cfg and store.
func NewSessionIssuer(cfg SessionConfig, store RefreshTokenStore) *SessionIssuer {
	return &SessionIssuer{cfg: cfg, refreshTokens: store}
}

// IssueAccessToken signs a short-lived JWT for userID.
func (s *SessionIssuer) IssueAccessToken(userID string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.cfg.AccessTokenTTL)

	claims := AccessClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Issuer:    s.cfg.Issuer,
			Audience:  jwt.ClaimStrings{s.cfg.Audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.cfg.SigningKey)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("auth: sign access token: %w", err)
	}

	return signed, expiresAt, nil
}

// IssueRefreshToken generates and persists a new opaque refresh token for userID.
func (s *SessionIssuer) IssueRefreshToken(ctx context.Context, userID string) (string, time.Time, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: generate refresh token: %w", err)
	}
	plaintext := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(s.cfg.RefreshTokenTTL)

	if _, err := s.refreshTokens.Create(ctx, userID, hashRefreshToken(plaintext), expiresAt); err != nil {
		return "", time.Time{}, fmt.Errorf("auth: persist refresh token: %w", err)
	}

	return plaintext, expiresAt, nil
}

// IssueSession issues a fresh access and refresh token pair for userID.
func (s *SessionIssuer) IssueSession(ctx context.Context, userID string) (*Session, error) {
	access, accessExpiresAt, err := s.IssueAccessToken(userID)
	if err != nil {
		return nil, err
	}

	refresh, refreshExpiresAt, err := s.IssueRefreshToken(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &Session{
		AccessToken:           access,
		AccessTokenExpiresAt:  accessExpiresAt,
		RefreshToken:          refresh,
		RefreshTokenExpiresAt: refreshExpiresAt,
	}, nil
}

// RotateRefreshToken exchanges a valid, unexpired refresh token for a new
// session, revoking the old refresh token so it cannot be replayed.
func (s *SessionIssuer) RotateRefreshToken(ctx context.Context, plaintext string) (*Session, error) {
	stored, err := s.refreshTokens.GetByHash(ctx, hashRefreshToken(plaintext))
	if err != nil {
		return nil, fmt.Errorf("auth: rotate refresh token: %w", err)
	}
	if stored.RevokedAt != nil {
		return nil, fmt.Errorf("auth: rotate refresh token: %w", db.ErrNotFound)
	}
	if time.Now().After(stored.ExpiresAt) {
		return nil, fmt.Errorf("auth: rotate refresh token: %w", db.ErrNotFound)
	}

	if err := s.refreshTokens.Revoke(ctx, stored.ID); err != nil {
		return nil, fmt.Errorf("auth: revoke rotated refresh token: %w", err)
	}

	return s.IssueSession(ctx, stored.UserID)
}

// RevokeRefreshToken invalidates a refresh token, such as on logout.
func (s *SessionIssuer) RevokeRefreshToken(ctx context.Context, plaintext string) error {
	stored, err := s.refreshTokens.GetByHash(ctx, hashRefreshToken(plaintext))
	if err != nil {
		return fmt.Errorf("auth: revoke refresh token: %w", err)
	}
	if err := s.refreshTokens.Revoke(ctx, stored.ID); err != nil {
		return fmt.Errorf("auth: revoke refresh token: %w", err)
	}
	return nil
}

func hashRefreshToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}
