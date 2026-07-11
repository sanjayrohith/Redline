package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/redline/internal/policy"
)

type fakeTOSAccepter struct {
	accepted []string
}

func (f *fakeTOSAccepter) AcceptTOS(_ context.Context, userID string) error {
	f.accepted = append(f.accepted, userID)
	return nil
}

func TestAcceptTOSHandler_RecordsAcceptanceForTheSessionUser(t *testing.T) {
	users := &fakeTOSAccepter{}
	handler := AcceptTOSHandler(users)

	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/tos/accept", nil)
	req = withPrincipal(req, "user-1")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if len(users.accepted) != 1 || users.accepted[0] != "user-1" {
		t.Errorf("accepted = %v, want [user-1]", users.accepted)
	}
}

func TestAcceptTOSHandler_RequiresSession(t *testing.T) {
	handler := AcceptTOSHandler(&fakeTOSAccepter{})
	req := httptest.NewRequest(http.MethodPost, "/v1/dashboard/tos/accept", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

type fakeSuspensionEnforcer struct {
	suspendedFor string
	result       *policy.Result
	err          error
}

func (f *fakeSuspensionEnforcer) Suspend(_ context.Context, userID string) (*policy.Result, error) {
	f.suspendedFor = userID
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func TestSuspendUserHandler_ReturnsSuspensionSummary(t *testing.T) {
	enforcer := &fakeSuspensionEnforcer{result: &policy.Result{
		RevokedKeyCount: 2, TerminatedDeploymentIDs: []string{"dep-1"}, StopJobErrors: map[string]error{},
	}}
	mux := http.NewServeMux()
	mux.Handle("POST /v1/admin/users/{id}/suspend", SuspendUserHandler(enforcer))

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/users/user-1/suspend", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if enforcer.suspendedFor != "user-1" {
		t.Errorf("suspendedFor = %q, want user-1", enforcer.suspendedFor)
	}
	var view SuspensionView
	if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if view.RevokedKeyCount != 2 || len(view.TerminatedDeploymentIDs) != 1 {
		t.Errorf("view = %+v", view)
	}
}

func TestSuspendUserHandler_RequiresID(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /v1/admin/users/{id}/suspend", SuspendUserHandler(&fakeSuspensionEnforcer{}))

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/users//suspend", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusOK {
		t.Fatal("status = 200, want a non-200 response for a missing user id")
	}
}
