package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/auth"
)

func testSessionConfig() auth.SessionConfig {
	return auth.SessionConfig{
		SigningKey:      []byte("test-signing-key"),
		Issuer:          "redline",
		Audience:        "redline-api",
		AccessTokenTTL:  15 * time.Minute,
		RefreshTokenTTL: 30 * 24 * time.Hour,
	}
}

func issueTestToken(t *testing.T, cfg auth.SessionConfig, userID string) string {
	t.Helper()
	issuer := auth.NewSessionIssuer(cfg, nil)
	token, _, err := issuer.IssueAccessToken(userID)
	if err != nil {
		t.Fatalf("IssueAccessToken() error = %v", err)
	}
	return token
}

func TestJWTAuth_ValidTokenAttachesPrincipal(t *testing.T) {
	cfg := testSessionConfig()
	token := issueTestToken(t, cfg, "user-1")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	rec, principal := serveThrough(JWTAuth(cfg), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if principal == nil || principal.UserID != "user-1" {
		t.Errorf("principal = %+v, want UserID user-1", principal)
	}
}

func TestJWTAuth_RejectsMissingToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec, principal := serveThrough(JWTAuth(testSessionConfig()), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if principal != nil {
		t.Error("principal should not be attached")
	}
	if got := rec.Body.String(); got != `{"error":"missing_token"}`+"\n" {
		t.Errorf("body = %q", got)
	}
}

func TestJWTAuth_RejectsMalformedToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec, _ := serveThrough(JWTAuth(testSessionConfig()), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"invalid_token"}`+"\n" {
		t.Errorf("body = %q, want invalid_token", got)
	}
}

func TestJWTAuth_RejectsExpiredToken(t *testing.T) {
	cfg := testSessionConfig()
	cfg.AccessTokenTTL = -time.Minute
	token := issueTestToken(t, cfg, "user-1")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, _ := serveThrough(JWTAuth(cfg), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Body.String(); got != `{"error":"token_expired"}`+"\n" {
		t.Errorf("body = %q, want token_expired", got)
	}
}

func TestJWTAuth_RejectsWrongSigningKey(t *testing.T) {
	cfg := testSessionConfig()
	token := issueTestToken(t, cfg, "user-1")

	wrongCfg := cfg
	wrongCfg.SigningKey = []byte("a-completely-different-key")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, _ := serveThrough(JWTAuth(wrongCfg), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestJWTAuth_RejectsWrongAudience(t *testing.T) {
	cfg := testSessionConfig()
	token := issueTestToken(t, cfg, "user-1")

	wrongCfg := cfg
	wrongCfg.Audience = "some-other-api"

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec, _ := serveThrough(JWTAuth(wrongCfg), req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
