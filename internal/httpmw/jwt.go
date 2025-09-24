package httpmw

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sanjayrohith/redline/internal/auth"
)

// JWTAuth validates the bearer access token's signature, expiry, audience,
// and issuer, attaching the resolved Principal to the request context on
// success. It distinguishes an expired token from a malformed or
// otherwise invalid one in its error response.
func JWTAuth(cfg auth.SessionConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeTokenError(w, "missing_token")
				return
			}

			claims := &auth.AccessClaims{}
			parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
				return cfg.SigningKey, nil
			},
				jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}),
				jwt.WithIssuer(cfg.Issuer),
				jwt.WithAudience(cfg.Audience),
			)

			switch {
			case errors.Is(err, jwt.ErrTokenExpired):
				writeTokenError(w, "token_expired")
				return
			case err != nil, !parsed.Valid:
				writeTokenError(w, "invalid_token")
				return
			}

			principal := &Principal{UserID: claims.Subject}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

func writeTokenError(w http.ResponseWriter, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}
