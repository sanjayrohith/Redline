// Package httpmw holds HTTP middleware for the gateway.
package httpmw

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/db"
)

// Principal is the identity resolved from a verified API key, attached to
// the request context for downstream handlers.
type Principal struct {
	APIKeyID string
	UserID   string
	OrgID    *string
	Scopes   []string
}

type principalContextKey struct{}

// WithPrincipal returns a context carrying p, retrievable via PrincipalFromContext.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

// PrincipalFromContext returns the Principal attached by APIKeyAuth, if any.
func PrincipalFromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(*Principal)
	return p, ok
}

// APIKeyLookup resolves an API key by its stored display prefix. It is
// satisfied by *db.APIKeyRepository; the interface exists so this
// middleware is testable without a real database.
type APIKeyLookup interface {
	GetByPrefix(ctx context.Context, prefix string) (*db.APIKey, error)
}

// APIKeyAuth resolves the bearer credential by its display prefix, verifies
// its Argon2id hash in constant time, and attaches the resolved Principal
// to the request context. Every failure mode - missing header, unknown
// prefix, revoked key, wrong key - produces the same uniform 401 so no
// response signal distinguishes them.
func APIKeyAuth(keys APIKeyLookup) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			candidate, ok := bearerToken(r)
			if !ok {
				unauthorized(w)
				return
			}

			record, err := keys.GetByPrefix(r.Context(), auth.DisplayPrefix(candidate))
			if err != nil {
				unauthorized(w)
				return
			}

			if record.RevokedAt != nil {
				unauthorized(w)
				return
			}

			valid, err := auth.VerifyAPIKey(candidate, record.KeyHash)
			if err != nil || !valid {
				unauthorized(w)
				return
			}

			principal := &Principal{
				APIKeyID: record.ID,
				UserID:   record.UserID,
				OrgID:    record.OrgID,
				Scopes:   record.Scopes,
			}

			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}

	token := strings.TrimPrefix(header, prefix)
	if token == "" {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
}
