package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/auth"
	"github.com/sanjayrohith/redline/internal/db"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

// AccessTokenCookie and RefreshTokenCookie are the httpOnly cookies a
// browser session is carried in. Neither is readable from JavaScript, so
// a stolen access token requires an XSS vulnerability that also defeats
// the cookie's HttpOnly flag itself, not merely one that reads
// browser-accessible storage such as localStorage.
const (
	// AccessTokenCookie holds the short-lived signed access token.
	AccessTokenCookie = "redline_access"
	// RefreshTokenCookie holds the opaque, longer-lived refresh token.
	RefreshTokenCookie = "redline_refresh"
)

// UserAuthenticator is the persistence dependency the auth handlers need.
// It is satisfied by *db.UserRepository.
type UserAuthenticator interface {
	GetByEmail(ctx context.Context, email string) (*db.User, error)
}

// SessionAuthority is the dependency the auth handlers need to mint and
// rotate sessions. It is satisfied by *auth.SessionIssuer.
type SessionAuthority interface {
	IssueSession(ctx context.Context, userID string) (*auth.Session, error)
	RotateRefreshToken(ctx context.Context, plaintext string) (*auth.Session, error)
	RevokeRefreshToken(ctx context.Context, plaintext string) error
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// AuthCookieOptions controls how session cookies are minted. Secure must
// be true in every environment except local development over plain HTTP,
// since a browser silently drops a Secure cookie set over HTTP.
type AuthCookieOptions struct {
	Secure bool
}

// LoginHandler implements POST /v1/auth/login: verifies email and
// password, then sets the access and refresh tokens as httpOnly cookies
// rather than returning them in the response body, so they are never
// accessible to page JavaScript.
func LoginHandler(users UserAuthenticator, sessions SessionAuthority, cookies AuthCookieOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "malformed request body", requestID)
			return
		}
		req.Email = strings.TrimSpace(req.Email)
		if req.Email == "" || req.Password == "" {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "email and password are required", requestID)
			return
		}

		user, err := users.GetByEmail(r.Context(), req.Email)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				apierror.Write(w, http.StatusUnauthorized, apierror.CodeUnauthorized, "invalid email or password", requestID)
				return
			}
			apierror.WriteStorageError(w, requestID, err)
			return
		}

		if !auth.VerifyPassword(user.PasswordHash, req.Password) {
			apierror.Write(w, http.StatusUnauthorized, apierror.CodeUnauthorized, "invalid email or password", requestID)
			return
		}

		session, err := sessions.IssueSession(r.Context(), user.ID)
		if err != nil {
			apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "failed to issue session", requestID)
			return
		}

		setSessionCookies(w, session, cookies)
		w.WriteHeader(http.StatusNoContent)
	}
}

// RefreshHandler implements POST /v1/auth/refresh: rotates the refresh
// token carried in RefreshTokenCookie for a fresh access/refresh pair,
// invalidating the old refresh token so it cannot be replayed.
func RefreshHandler(sessions SessionAuthority, cookies AuthCookieOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		cookie, err := r.Cookie(RefreshTokenCookie)
		if err != nil || cookie.Value == "" {
			apierror.Write(w, http.StatusUnauthorized, apierror.CodeUnauthorized, "missing refresh token", requestID)
			return
		}

		session, err := sessions.RotateRefreshToken(r.Context(), cookie.Value)
		if err != nil {
			clearSessionCookies(w, cookies)
			apierror.Write(w, http.StatusUnauthorized, apierror.CodeUnauthorized, "invalid or expired refresh token", requestID)
			return
		}

		setSessionCookies(w, session, cookies)
		w.WriteHeader(http.StatusNoContent)
	}
}

// LogoutHandler implements POST /v1/auth/logout: revokes the current
// refresh token and clears both session cookies.
func LogoutHandler(sessions SessionAuthority, cookies AuthCookieOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie(RefreshTokenCookie); err == nil && cookie.Value != "" {
			_ = sessions.RevokeRefreshToken(r.Context(), cookie.Value)
		}
		clearSessionCookies(w, cookies)
		w.WriteHeader(http.StatusNoContent)
	}
}

func setSessionCookies(w http.ResponseWriter, session *auth.Session, opts AuthCookieOptions) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is opts.Secure, a variable, not a literal true - false only in local development over plain HTTP
		Name:     AccessTokenCookie,
		Value:    session.AccessToken,
		Path:     "/",
		Expires:  session.AccessTokenExpiresAt,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is opts.Secure, a variable, not a literal true - false only in local development over plain HTTP
		Name:     RefreshTokenCookie,
		Value:    session.RefreshToken,
		Path:     "/v1/auth",
		Expires:  session.RefreshTokenExpiresAt,
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookies(w http.ResponseWriter, opts AuthCookieOptions) {
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is opts.Secure, a variable, not a literal true - false only in local development over plain HTTP
		Name: AccessTokenCookie, Value: "", Path: "/",
		MaxAge: -1, HttpOnly: true, Secure: opts.Secure, SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // G124: Secure is opts.Secure, a variable, not a literal true - false only in local development over plain HTTP
		Name: RefreshTokenCookie, Value: "", Path: "/v1/auth",
		MaxAge: -1, HttpOnly: true, Secure: opts.Secure, SameSite: http.SameSiteLaxMode,
	})
}
