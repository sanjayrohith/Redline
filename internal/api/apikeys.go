package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

// KeyStore is the persistence dependency the API key handlers need. It
// is satisfied by *db.APIKeyRepository.
type KeyStore interface {
	Create(ctx context.Context, key db.NewAPIKey) (*db.APIKey, error)
	ListByUser(ctx context.Context, userID string) ([]db.APIKey, error)
	Revoke(ctx context.Context, id string) error
}

// KeyView is one API key as returned to a client: KeyHash never
// appears here, and Key (the plaintext) is populated only in the
// CreateAPIKeyHandler response - the one moment the plaintext exists.
type KeyView struct {
	ID            string     `json:"id"`
	DisplayPrefix string     `json:"display_prefix"`
	Scopes        []string   `json:"scopes"`
	LastUsedAt    *time.Time `json:"last_used_at,omitempty"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	Key           string     `json:"key,omitempty"`
}

type createAPIKeyRequest struct {
	Scopes []string `json:"scopes"`
}

// CreateAPIKeyHandler implements POST /v1/api-keys: generates a new key
// for the authenticated principal and returns its plaintext exactly once.
func CreateAPIKeyHandler(keys KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())
		principal, ok := httpmw.PrincipalFromContext(r.Context())
		if !ok {
			apierror.Write(w, http.StatusUnauthorized, apierror.CodeUnauthorized, "missing session", requestID)
			return
		}

		var req createAPIKeyRequest
		if r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "malformed request body", requestID)
				return
			}
		}

		generated, err := auth.GenerateAPIKey()
		if err != nil {
			apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "failed to generate key", requestID)
			return
		}

		record, err := keys.Create(r.Context(), db.NewAPIKey{
			UserID:        principal.UserID,
			KeyHash:       generated.Hash,
			DisplayPrefix: generated.DisplayPrefix,
			Scopes:        req.Scopes,
		})
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(KeyView{
			ID: record.ID, DisplayPrefix: record.DisplayPrefix, Scopes: record.Scopes,
			CreatedAt: record.CreatedAt, Key: generated.Plaintext,
		})
	}
}

// ListAPIKeysHandler implements GET /v1/api-keys: every key belonging to
// the authenticated principal, showing only each key's display prefix.
func ListAPIKeysHandler(keys KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())
		principal, ok := httpmw.PrincipalFromContext(r.Context())
		if !ok {
			apierror.Write(w, http.StatusUnauthorized, apierror.CodeUnauthorized, "missing session", requestID)
			return
		}

		records, err := keys.ListByUser(r.Context(), principal.UserID)
		if err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		views := make([]KeyView, len(records))
		for i, k := range records {
			views[i] = KeyView{
				ID: k.ID, DisplayPrefix: k.DisplayPrefix, Scopes: k.Scopes,
				LastUsedAt: k.LastUsedAt, RevokedAt: k.RevokedAt, CreatedAt: k.CreatedAt,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(struct {
			Data []KeyView `json:"data"`
		}{Data: views})
	}
}

// RevokeAPIKeyHandler implements DELETE /v1/api-keys/{id}: revokes the
// key, which the caller identifies by its opaque id, never its hash.
func RevokeAPIKeyHandler(keys KeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		id := r.PathValue("id")
		if id == "" {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "missing key id", requestID)
			return
		}

		if err := keys.Revoke(r.Context(), id); err != nil {
			apierror.WriteStorageError(w, requestID, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
