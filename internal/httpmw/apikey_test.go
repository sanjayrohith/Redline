package httpmw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/db"
)

type fakeAPIKeyLookup struct {
	byPrefix map[string]*db.APIKey
}

func (f *fakeAPIKeyLookup) GetByPrefix(_ context.Context, prefix string) (*db.APIKey, error) {
	if key, ok := f.byPrefix[prefix]; ok {
		return key, nil
	}
	return nil, db.ErrNotFound
}

func newFixture(t *testing.T) (*fakeAPIKeyLookup, *auth.GeneratedKey, *db.APIKey) {
	t.Helper()

	generated, err := auth.GenerateAPIKey()
	if err != nil {
		t.Fatalf("GenerateAPIKey() error = %v", err)
	}

	record := &db.APIKey{
		ID:            "key-1",
		UserID:        "user-1",
		KeyHash:       generated.Hash,
		DisplayPrefix: generated.DisplayPrefix,
		Scopes:        []string{"inference"},
	}

	lookup := &fakeAPIKeyLookup{byPrefix: map[string]*db.APIKey{generated.DisplayPrefix: record}}
	return lookup, generated, record
}

func serveThrough(mw func(http.Handler) http.Handler, req *http.Request) (*httptest.ResponseRecorder, *Principal) {
	var seen *Principal
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, seen
}

func TestAPIKeyAuth_ValidKeyAttachesPrincipal(t *testing.T) {
	lookup, generated, record := newFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+generated.Plaintext)

	rec, principal := serveThrough(APIKeyAuth(lookup), req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if principal == nil {
		t.Fatal("principal was not attached to context")
	}
	if principal.UserID != record.UserID {
		t.Errorf("UserID = %q, want %q", principal.UserID, record.UserID)
	}
}

func TestAPIKeyAuth_RejectsMissingHeader(t *testing.T) {
	lookup, _, _ := newFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec, principal := serveThrough(APIKeyAuth(lookup), req)

	assertUniform401(t, rec, principal)
}

func TestAPIKeyAuth_RejectsUnknownPrefix(t *testing.T) {
	lookup, _, _ := newFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer rl_totally-unknown-key")
	rec, principal := serveThrough(APIKeyAuth(lookup), req)

	assertUniform401(t, rec, principal)
}

func TestAPIKeyAuth_RejectsWrongKeyForKnownPrefix(t *testing.T) {
	lookup, generated, _ := newFixture(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+generated.DisplayPrefix+"-not-the-real-suffix")
	rec, principal := serveThrough(APIKeyAuth(lookup), req)

	assertUniform401(t, rec, principal)
}

func TestAPIKeyAuth_RejectsRevokedKey(t *testing.T) {
	lookup, generated, record := newFixture(t)
	now := time.Now()
	record.RevokedAt = &now

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+generated.Plaintext)
	rec, principal := serveThrough(APIKeyAuth(lookup), req)

	assertUniform401(t, rec, principal)
}

func assertUniform401(t *testing.T, rec *httptest.ResponseRecorder, principal *Principal) {
	t.Helper()

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if principal != nil {
		t.Error("principal should not be attached on a rejected request")
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Error.Code != "unauthorized" {
		t.Errorf("error.code = %q, want unauthorized", body.Error.Code)
	}
}
