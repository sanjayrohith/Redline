package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/db"
)

type fakeUserAuthenticator struct {
	usersByEmail map[string]*db.User
}

func (f *fakeUserAuthenticator) GetByEmail(_ context.Context, email string) (*db.User, error) {
	u, ok := f.usersByEmail[email]
	if !ok {
		return nil, db.ErrNotFound
	}
	return u, nil
}

type fakeSessionAuthority struct {
	issuedFor string
	revoked   []string
	rotateErr error
}

func (f *fakeSessionAuthority) IssueSession(_ context.Context, userID string) (*auth.Session, error) {
	f.issuedFor = userID
	return &auth.Session{
		AccessToken: "access-" + userID, AccessTokenExpiresAt: time.Now().Add(time.Minute),
		RefreshToken: "refresh-" + userID, RefreshTokenExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (f *fakeSessionAuthority) RotateRefreshToken(_ context.Context, plaintext string) (*auth.Session, error) {
	if f.rotateErr != nil {
		return nil, f.rotateErr
	}
	return &auth.Session{
		AccessToken: "rotated-access", AccessTokenExpiresAt: time.Now().Add(time.Minute),
		RefreshToken: "rotated-refresh-for-" + plaintext, RefreshTokenExpiresAt: time.Now().Add(time.Hour),
	}, nil
}

func (f *fakeSessionAuthority) RevokeRefreshToken(_ context.Context, plaintext string) error {
	f.revoked = append(f.revoked, plaintext)
	return nil
}

func TestLoginHandler_SetsHttpOnlyCookiesOnSuccess(t *testing.T) {
	hash, err := auth.HashPassword("correct-horse")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	users := &fakeUserAuthenticator{usersByEmail: map[string]*db.User{
		"user@example.com": {ID: "user-1", Email: "user@example.com", PasswordHash: hash},
	}}
	sessions := &fakeSessionAuthority{}

	handler := LoginHandler(users, sessions, AuthCookieOptions{Secure: true})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"user@example.com","password":"correct-horse"}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body = %s", rec.Code, rec.Body.String())
	}
	if sessions.issuedFor != "user-1" {
		t.Errorf("issuedFor = %q, want user-1", sessions.issuedFor)
	}

	res := rec.Result()
	var sawAccess, sawRefresh bool
	for _, c := range res.Cookies() {
		if c.Name == AccessTokenCookie {
			sawAccess = true
			if !c.HttpOnly || !c.Secure {
				t.Errorf("access cookie HttpOnly=%v Secure=%v, want both true", c.HttpOnly, c.Secure)
			}
		}
		if c.Name == RefreshTokenCookie {
			sawRefresh = true
			if !c.HttpOnly || !c.Secure {
				t.Errorf("refresh cookie HttpOnly=%v Secure=%v, want both true", c.HttpOnly, c.Secure)
			}
		}
	}
	if !sawAccess || !sawRefresh {
		t.Errorf("sawAccess=%v sawRefresh=%v, want both true", sawAccess, sawRefresh)
	}
}

func TestLoginHandler_RejectsWrongPassword(t *testing.T) {
	hash, _ := auth.HashPassword("correct-horse")
	users := &fakeUserAuthenticator{usersByEmail: map[string]*db.User{
		"user@example.com": {ID: "user-1", Email: "user@example.com", PasswordHash: hash},
	}}
	handler := LoginHandler(users, &fakeSessionAuthority{}, AuthCookieOptions{})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"user@example.com","password":"wrong"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginHandler_RejectsUnknownEmail(t *testing.T) {
	handler := LoginHandler(&fakeUserAuthenticator{usersByEmail: map[string]*db.User{}}, &fakeSessionAuthority{}, AuthCookieOptions{})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"nobody@example.com","password":"whatever"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLoginHandler_UserWithNoPasswordNeverAuthenticates(t *testing.T) {
	users := &fakeUserAuthenticator{usersByEmail: map[string]*db.User{
		"nopassword@example.com": {ID: "user-2", Email: "nopassword@example.com", PasswordHash: ""},
	}}
	handler := LoginHandler(users, &fakeSessionAuthority{}, AuthCookieOptions{})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"nopassword@example.com","password":""}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for empty password", rec.Code)
	}
}

func TestRefreshHandler_RotatesAndSetsCookies(t *testing.T) {
	sessions := &fakeSessionAuthority{}
	handler := RefreshHandler(sessions, AuthCookieOptions{Secure: true})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: RefreshTokenCookie, Value: "old-refresh-token"}) //nolint:gosec // G124: incoming request cookie in a test, not a Set-Cookie response
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}

	var newRefresh string
	for _, c := range rec.Result().Cookies() {
		if c.Name == RefreshTokenCookie {
			newRefresh = c.Value
		}
	}
	if newRefresh != "rotated-refresh-for-old-refresh-token" {
		t.Errorf("rotated refresh cookie = %q, want it derived from the old token", newRefresh)
	}
}

func TestRefreshHandler_MissingCookieIsUnauthorized(t *testing.T) {
	handler := RefreshHandler(&fakeSessionAuthority{}, AuthCookieOptions{})
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLogoutHandler_RevokesAndClearsCookies(t *testing.T) {
	sessions := &fakeSessionAuthority{}
	handler := LogoutHandler(sessions, AuthCookieOptions{})

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: RefreshTokenCookie, Value: "some-refresh-token"}) //nolint:gosec // G124: incoming request cookie in a test, not a Set-Cookie response
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if len(sessions.revoked) != 1 || sessions.revoked[0] != "some-refresh-token" {
		t.Errorf("revoked = %v, want [some-refresh-token]", sessions.revoked)
	}

	for _, c := range rec.Result().Cookies() {
		if c.MaxAge >= 0 {
			t.Errorf("cookie %s MaxAge = %d, want negative (cleared)", c.Name, c.MaxAge)
		}
	}
}
