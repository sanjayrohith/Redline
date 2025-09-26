package httpmw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func withPrincipalRequest(p *Principal) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/v1/deployments", nil)
	if p != nil {
		req = req.WithContext(WithPrincipal(context.Background(), p))
	}
	return req
}

func TestRequireScope_AllowsMatchingScope(t *testing.T) {
	handler := RequireScope("deployments:write")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipalRequest(&Principal{Scopes: []string{"inference", "deployments:write"}}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRequireScope_RejectsMissingScope(t *testing.T) {
	handler := RequireScope("deployments:write")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipalRequest(&Principal{Scopes: []string{"inference"}}))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	if got := errorCode(t, rec); got != "forbidden" {
		t.Errorf("error.code = %q, want forbidden", got)
	}
}

func TestRequireScope_RejectsNoPrincipal(t *testing.T) {
	handler := RequireScope("deployments:write")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, withPrincipalRequest(nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
