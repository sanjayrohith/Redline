package policy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/httpmw"
)

// chatRequestBody is the minimal shape AbuseGuardMiddleware needs from a
// chat completion request body to score its prompt's entropy - it does
// not need or decode the rest of the request.
type chatRequestBody struct {
	Messages []struct {
		Content string `json:"content"`
	} `json:"messages"`
}

// AbuseGuardMiddleware runs AbuseDetector against every request from an
// authenticated principal, escalates through escalator on any flagged
// signal, and enforces an already-active rate reduction (a stricter,
// independent budget layered in front of the route's normal rate
// limiting) before the request reaches the handler.
func AbuseGuardMiddleware(detector *AbuseDetector, escalator *Escalator, reduced *RateReductionStore, reducedLimit int, reducedWindow time.Duration, reducedSignalLimiter SignalLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID, _ := httpmw.RequestIDFromContext(r.Context())
			principal, ok := httpmw.PrincipalFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			userID := principal.UserID

			isReduced, err := reduced.IsReduced(r.Context(), userID)
			if err == nil && isReduced {
				allowed, err := reducedSignalLimiter.Allow(r.Context(), "abuse:reducedbudget:"+userID, reducedLimit, reducedWindow)
				if err == nil && !allowed {
					apierror.Write(w, http.StatusTooManyRequests, apierror.CodeRateLimited,
						"this account's rate limit has been reduced due to detected abuse patterns", requestID)
					return
				}
			}

			body, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))

			var prompt string
			var parsed chatRequestBody
			if json.Unmarshal(body, &parsed) == nil {
				for _, m := range parsed.Messages {
					prompt += m.Content
				}
			}

			flagged, _ := detector.CheckRequestCadence(r.Context(), userID)
			if !flagged && prompt != "" {
				flagged, _ = detector.CheckPromptEntropy(r.Context(), userID, prompt)
			}
			if flagged {
				_, _ = escalator.RecordViolation(r.Context(), userID)
			}

			next.ServeHTTP(w, r)
		})
	}
}
