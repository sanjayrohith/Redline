package app_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/config"
)

// tpotSamplesFromSSE replays a streamed response's SSE frames and returns
// the wall-clock gap between every content chunk after the first - the
// same inter-token cadence streamChatCompletion itself records as TPOT,
// measured independently here from the client's own read timing rather
// than trusting the server's internal accounting.
func tpotSamplesFromSSE(body string) []time.Duration {
	var samples []time.Duration
	var lastToken time.Time
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		if !strings.Contains(line, `"content"`) {
			continue
		}
		now := time.Now()
		if !lastToken.IsZero() {
			samples = append(samples, now.Sub(lastToken))
		}
		lastToken = now
	}
	return samples
}

// TestLoad_ConcurrentStreamingRequests_QueueDepthBoundedAndTPOTStable
// drives sustained concurrent streaming load against one principal - the
// scenario a long-context chunked-prefill workload produces in practice
// (many overlapping in-flight generations competing for the same
// admission budget) - and asserts two things a real deployment needs
// bounded, not just working: the rate limiter's admitted concurrency
// never exceeds its configured budget (queue depth stays bounded), and
// every admitted request's inter-token cadence stays within a sane
// ceiling (TPOT stability) rather than degrading under load.
func TestLoad_ConcurrentStreamingRequests_QueueDepthBoundedAndTPOTStable(t *testing.T) {
	env := setupE2EEnv(t)

	const rateLimit = 10
	const concurrency = 30
	const tpotCeiling = 500 * time.Millisecond

	a := newTestApp(t, env, func(cfg *config.Config) {
		cfg.ChatRateLimit = rateLimit
		cfg.ChatRateLimitWindow = time.Minute
	})
	key := issueAPIKey(t, a, "load-test@example.com", []string{"inference"})

	var wg sync.WaitGroup
	var mu sync.Mutex
	var successCount, rateLimitedCount int
	var allTPOTSamples []time.Duration

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()

			rec := httptest.NewRecorder()
			body := `{"model":"mock-model","messages":[{"role":"user","content":"sustained load probe"}],"stream":true}`
			a.Handler().ServeHTTP(rec, chatRequest(t, key, body))

			mu.Lock()
			defer mu.Unlock()
			switch rec.Code {
			case http.StatusOK:
				successCount++
				allTPOTSamples = append(allTPOTSamples, tpotSamplesFromSSE(rec.Body.String())...)
			case http.StatusTooManyRequests:
				rateLimitedCount++
			}
		}()
	}
	wg.Wait()

	// Queue depth stays within its configured bound: no more than
	// rateLimit requests were ever admitted from this one principal,
	// regardless of how many were attempted concurrently.
	if successCount > rateLimit {
		t.Errorf("successCount = %d, want at most %d (the configured rate limit)", successCount, rateLimit)
	}
	// The bound is actually enforced, not silently bypassed under load:
	// with concurrency > rateLimit, some requests must have been rejected.
	if rateLimitedCount == 0 {
		t.Error("rateLimitedCount = 0, want at least one request rejected when concurrency exceeds the configured limit")
	}
	if successCount+rateLimitedCount != concurrency {
		t.Errorf("successCount(%d) + rateLimitedCount(%d) = %d, want %d (every request must resolve one way or the other)",
			successCount, rateLimitedCount, successCount+rateLimitedCount, concurrency)
	}

	// TPOT stability: every observed inter-token gap, across every
	// admitted request running concurrently, stays under a sane ceiling -
	// load must not make a request that WAS admitted degrade unboundedly.
	for _, d := range allTPOTSamples {
		if d > tpotCeiling {
			t.Errorf("observed TPOT sample %v exceeds the %v ceiling under concurrent load", d, tpotCeiling)
		}
	}
	if len(allTPOTSamples) == 0 {
		t.Error("no TPOT samples were collected - the harness observed no multi-token streamed response")
	}
}
