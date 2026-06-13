package httpmw

import (
	"errors"
	"net/http"

	"github.com/golang-jwt/jwt/v5"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/auth"
)

// AccessTokenCookieName is the httpOnly cookie a browser session's access
// token is carried in - the same name api.AccessTokenCookie sets, defined
// here (a package api already depends on) rather than there, so JWTAuth
// can read it without an import cycle.
const AccessTokenCookieName = "redline_access"

// JWTAuth validates the bearer access token's signature, expiry, audience,
// and issuer, attaching the resolved Principal to the request context on
// success. It distinguishes an expired token from a malformed or
// otherwise invalid one in its error response. The token is read from the
// Authorization header first (a programmatic caller) and, failing that,
// from the access-token cookie (a browser session) - covering both
// callers this middleware protects routes for.
func JWTAuth(cfg auth.SessionConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := sessionToken(r)
			if !ok {
				writeTokenError(w, r, apierror.CodeUnauthorized, "missing bearer token")
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
				writeTokenError(w, r, apierror.CodeTokenExpired, "access token expired")
				return
			case err != nil, !parsed.Valid:
				writeTokenError(w, r, apierror.CodeInvalidToken, "invalid access token")
				return
			}

			principal := &Principal{UserID: claims.Subject}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

func writeTokenError(w http.ResponseWriter, r *http.Request, code apierror.Code, message string) {
	requestID, _ := RequestIDFromContext(r.Context())
	apierror.Write(w, http.StatusUnauthorized, code, message, requestID)
}

// sessionToken resolves the caller's access token from, in order: the
// Authorization header (a programmatic caller), the access-token cookie
// (a same-origin browser page), or an access_token query parameter (a
// WebSocket upgrade request, which cannot carry a custom header and -
// once the connection crosses to a different origin than the one that
// set the cookie, as the gateway's own origin does relative to the
// frontend's - cannot rely on the cookie either).
func sessionToken(r *http.Request) (string, bool) {
	if token, ok := bearerToken(r); ok {
		return token, true
	}
	if cookie, err := r.Cookie(AccessTokenCookieName); err == nil && cookie.Value != "" {
		return cookie.Value, true
	}
	if token := r.URL.Query().Get("access_token"); token != "" {
		return token, true
	}
	return "", false
}
