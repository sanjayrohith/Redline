// Package apierror declares the gateway's single error response shape and
// maps internal error classes onto it, so every failure - auth, validation,
// rate limiting, timeouts, storage - reaches the client in one consistent
// envelope.
package apierror

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sanjayrohith/redline/internal/db"
)

// Code is a stable, machine-readable error identifier.
type Code string

// The full set of canonical error codes the gateway returns.
const (
	CodeUnauthorized Code = "unauthorized"
	CodeTokenExpired Code = "token_expired"
	CodeInvalidToken Code = "invalid_token"
	CodeForbidden    Code = "forbidden"
	CodeNotFound     Code = "not_found"
	CodeConflict     Code = "conflict"
	CodeValidation   Code = "validation_error"
	CodeRateLimited  Code = "rate_limited"
	CodeTimeout      Code = "request_timeout"
	CodeInternal     Code = "internal_error"
)

// Envelope is the single shape every gateway error response takes.
type Envelope struct {
	Error Detail `json:"error"`
}

// Detail carries the machine-readable code, a human message, and the
// request id so a client and an operator can correlate a failure back to
// one request.
type Detail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

// Write sends status with the canonical error envelope.
func Write(w http.ResponseWriter, status int, code Code, message, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Envelope{
		Error: Detail{Code: string(code), Message: message, RequestID: requestID},
	})
}

// WriteStorageError maps a repository error onto the canonical envelope:
// db.ErrNotFound to 404, db.ErrConflict to 409, and anything else to a
// generic 500 that does not leak internal detail to the client.
func WriteStorageError(w http.ResponseWriter, requestID string, err error) {
	switch {
	case errors.Is(err, db.ErrNotFound):
		Write(w, http.StatusNotFound, CodeNotFound, "resource not found", requestID)
	case errors.Is(err, db.ErrConflict):
		Write(w, http.StatusConflict, CodeConflict, "resource already exists", requestID)
	default:
		Write(w, http.StatusInternalServerError, CodeInternal, "internal server error", requestID)
	}
}
