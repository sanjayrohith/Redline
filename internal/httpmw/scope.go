package httpmw

import (
	"encoding/json"
	"net/http"
)

// RequireScope rejects any request whose Principal (attached by APIKeyAuth)
// does not carry the given scope. It must run after APIKeyAuth in the
// middleware chain; a session (JWT) principal carries no scopes and is
// always rejected by this check, since scoping is an API-key concept.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, ok := PrincipalFromContext(r.Context())
			if !ok || !hasScope(principal.Scopes, scope) {
				forbidden(w)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

func forbidden(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
}
