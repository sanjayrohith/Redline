package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/policy"
)

// TOSAccepter records that the authenticated principal accepted the
// current terms of service. It is satisfied by *db.UserRepository.
type TOSAccepter interface {
	AcceptTOS(ctx context.Context, userID string) error
}

// AcceptTOSHandler implements POST /v1/dashboard/tos/accept: records
// acceptance for the authenticated session's user.
func AcceptTOSHandler(users TOSAccepter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		principal, ok := httpmw.PrincipalFromContext(r.Context())
		if !ok {
			apierror.Write(w, http.StatusUnauthorized, apierror.CodeUnauthorized, "missing session", requestID)
			return
		}

		if err := users.AcceptTOS(r.Context(), principal.UserID); err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// SuspensionEnforcer immediately suspends a user account. It is satisfied
// by *policy.Enforcer.
type SuspensionEnforcer interface {
	Suspend(ctx context.Context, userID string) (*policy.Result, error)
}

// SuspensionView is the HTTP response for a completed suspension.
type SuspensionView struct {
	RevokedKeyCount         int64    `json:"revoked_key_count"`
	TerminatedDeploymentIDs []string `json:"terminated_deployment_ids"`
	StopJobFailureCount     int      `json:"stop_job_failure_count"`
}

// SuspendUserHandler implements POST /v1/admin/users/{id}/suspend:
// immediately suspends the account - revoking every API key and
// terminating every live deployment it owns. Gated behind an "admin"
// scope, since this action affects another principal entirely, not the
// caller's own account.
func SuspendUserHandler(enforcer SuspensionEnforcer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		id := r.PathValue("id")
		if id == "" {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "missing user id", requestID)
			return
		}

		result, err := enforcer.Suspend(r.Context(), id)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(SuspensionView{
			RevokedKeyCount:         result.RevokedKeyCount,
			TerminatedDeploymentIDs: result.TerminatedDeploymentIDs,
			StopJobFailureCount:     len(result.StopJobErrors),
		})
	}
}
