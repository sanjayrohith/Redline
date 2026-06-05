package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

type fakeKeyStore struct {
	created []db.NewAPIKey
	listed  []db.APIKey
	revoked []string
	nextID  string
}

func (f *fakeKeyStore) Create(_ context.Context, key db.NewAPIKey) (*db.APIKey, error) {
	f.created = append(f.created, key)
	return &db.APIKey{
		ID: f.nextID, UserID: key.UserID, KeyHash: key.KeyHash,
		DisplayPrefix: key.DisplayPrefix, Scopes: key.Scopes, CreatedAt: time.Now(),
	}, nil
}

func (f *fakeKeyStore) ListByUser(_ context.Context, _ string) ([]db.APIKey, error) {
	return f.listed, nil
}

func (f *fakeKeyStore) Revoke(_ context.Context, id string) error {
	f.revoked = append(f.revoked, id)
	return nil
}

func withPrincipal(req *http.Request, userID string) *http.Request {
	return req.WithContext(httpmw.WithPrincipal(req.Context(), &httpmw.Principal{UserID: userID}))
}

func TestCreateAPIKeyHandler_ReturnsPlaintextOnce(t *testing.T) {
	store := &fakeKeyStore{nextID: "key-1"}
	handler := CreateAPIKeyHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", strings.NewReader(`{"scopes":["chat"]}`))
	req = withPrincipal(req, "user-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body = %s", rec.Code, rec.Body.String())
	}
	var view KeyView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.Key == "" {
		t.Error("Key is empty, want the plaintext key on creation")
	}
	if !strings.HasPrefix(view.Key, view.DisplayPrefix) {
		t.Errorf("Key %q does not start with DisplayPrefix %q", view.Key, view.DisplayPrefix)
	}
	if len(store.created) != 1 || store.created[0].UserID != "user-1" {
		t.Errorf("created = %+v, want one key for user-1", store.created)
	}
}

func TestCreateAPIKeyHandler_RequiresPrincipal(t *testing.T) {
	handler := CreateAPIKeyHandler(&fakeKeyStore{})
	req := httptest.NewRequest(http.MethodPost, "/v1/api-keys", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestListAPIKeysHandler_NeverExposesHash(t *testing.T) {
	store := &fakeKeyStore{listed: []db.APIKey{
		{ID: "key-1", DisplayPrefix: "rl_abcd1234", KeyHash: "super-secret-hash", CreatedAt: time.Now()},
	}}
	handler := ListAPIKeysHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/v1/api-keys", nil)
	req = withPrincipal(req, "user-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "super-secret-hash") {
		t.Error("response body leaks the key hash")
	}
	if !strings.Contains(rec.Body.String(), "rl_abcd1234") {
		t.Error("response body missing the display prefix")
	}
}

func TestRevokeAPIKeyHandler_RevokesByID(t *testing.T) {
	store := &fakeKeyStore{}
	mux := http.NewServeMux()
	mux.Handle("DELETE /v1/api-keys/{id}", RevokeAPIKeyHandler(store))

	req := httptest.NewRequest(http.MethodDelete, "/v1/api-keys/key-1", nil)
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body = %s", rec.Code, rec.Body.String())
	}
	if len(store.revoked) != 1 || store.revoked[0] != "key-1" {
		t.Errorf("revoked = %v, want [key-1]", store.revoked)
	}
}
