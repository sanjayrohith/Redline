package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sanjayrohith/redline/internal/db"
)

type fakeRefreshTokenStore struct {
	byHash map[string]*db.RefreshToken
	nextID int
}

func newFakeRefreshTokenStore() *fakeRefreshTokenStore {
	return &fakeRefreshTokenStore{byHash: map[string]*db.RefreshToken{}}
}

func (f *fakeRefreshTokenStore) Create(_ context.Context, userID, tokenHash string, expiresAt time.Time) (*db.RefreshToken, error) {
	f.nextID++
	t := &db.RefreshToken{
		ID:        fmt.Sprintf("token-%d", f.nextID),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now(),
	}
	f.byHash[tokenHash] = t
	return t, nil
}

func (f *fakeRefreshTokenStore) GetByHash(_ context.Context, tokenHash string) (*db.RefreshToken, error) {
	if t, ok := f.byHash[tokenHash]; ok {
		return t, nil
	}
	return nil, db.ErrNotFound
}

func (f *fakeRefreshTokenStore) Revoke(_ context.Context, id string) error {
	for _, t := range f.byHash {
		if t.ID == id {
			if t.RevokedAt != nil {
				return db.ErrNotFound
			}
			now := time.Now()
			t.RevokedAt = &now
			return nil
		}
	}
	return db.ErrNotFound
}

func testConfig() SessionConfig {
	return SessionConfig{
		SigningKey:      []byte("test-signing-key"),
		Issuer:          "redline",
		Audience:        "redline-api",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}
}

func TestIssueAccessToken(t *testing.T) {
	issuer := NewSessionIssuer(testConfig(), newFakeRefreshTokenStore())

	token, expiresAt, err := issuer.IssueAccessToken("user-1")
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	if token == "" {
		t.Fatal("token is empty")
	}
	if !expiresAt.After(time.Now()) {
		t.Error("expiresAt should be in the future")
	}

	parsed, err := jwt.ParseWithClaims(token, &AccessClaims{}, func(*jwt.Token) (any, error) {
		return testConfig().SigningKey, nil
	})
	if err != nil {
		t.Fatalf("parse issued token: %v", err)
	}
	claims := parsed.Claims.(*AccessClaims)
	if claims.Subject != "user-1" {
		t.Errorf("Subject = %q, want user-1", claims.Subject)
	}
	if claims.Issuer != "redline" {
		t.Errorf("Issuer = %q, want redline", claims.Issuer)
	}
}

func TestIssueSessionAndRotate(t *testing.T) {
	store := newFakeRefreshTokenStore()
	issuer := NewSessionIssuer(testConfig(), store)
	ctx := context.Background()

	session, err := issuer.IssueSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("IssueSession() error = %v", err)
	}
	if session.RefreshToken == "" {
		t.Fatal("RefreshToken is empty")
	}

	rotated, err := issuer.RotateRefreshToken(ctx, session.RefreshToken)
	if err != nil {
		t.Fatalf("RotateRefreshToken() error = %v", err)
	}
	if rotated.RefreshToken == session.RefreshToken {
		t.Error("rotation should mint a new refresh token")
	}

	if _, err := issuer.RotateRefreshToken(ctx, session.RefreshToken); err == nil {
		t.Error("rotating an already-rotated refresh token should fail")
	}
}

func TestRevokeRefreshToken(t *testing.T) {
	store := newFakeRefreshTokenStore()
	issuer := NewSessionIssuer(testConfig(), store)
	ctx := context.Background()

	session, err := issuer.IssueSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("IssueSession() error = %v", err)
	}

	if err := issuer.RevokeRefreshToken(ctx, session.RefreshToken); err != nil {
		t.Fatalf("RevokeRefreshToken() error = %v", err)
	}

	if _, err := issuer.RotateRefreshToken(ctx, session.RefreshToken); err == nil {
		t.Error("rotating a revoked refresh token should fail")
	}
}

func TestRotateRefreshToken_RejectsExpired(t *testing.T) {
	store := newFakeRefreshTokenStore()
	cfg := testConfig()
	cfg.RefreshTokenTTL = -time.Hour // already expired
	issuer := NewSessionIssuer(cfg, store)
	ctx := context.Background()

	session, err := issuer.IssueSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("IssueSession() error = %v", err)
	}

	if _, err := issuer.RotateRefreshToken(ctx, session.RefreshToken); err == nil {
		t.Error("rotating an expired refresh token should fail")
	}
}
