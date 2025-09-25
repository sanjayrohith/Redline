// Package ratelimit implements a Redis-backed sliding window rate limiter.
package ratelimit

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// slidingWindowScript evaluates the entire check-and-record decision in one
// atomic round trip: prune entries older than the window, count what
// remains, and either admit (recording this request) or reject.
const slidingWindowScript = `
local key = KEYS[1]
local now_ms = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, '-inf', now_ms - window_ms)
local count = redis.call('ZCARD', key)

if count >= limit then
	local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
	local retry_after_ms = window_ms
	if oldest[2] ~= nil then
		retry_after_ms = (tonumber(oldest[2]) + window_ms) - now_ms
	end
	return {0, retry_after_ms}
end

redis.call('ZADD', key, now_ms, member)
redis.call('PEXPIRE', key, window_ms)
return {1, 0}
`

// redisClient is the subset of *redisclient.Client the limiter needs.
type redisClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
}

// Limiter enforces a sliding-window request rate against Redis.
type Limiter struct {
	client redisClient
}

// NewLimiter returns a Limiter backed by client.
func NewLimiter(client redisClient) *Limiter {
	return &Limiter{client: client}
}

// Decision is the outcome of one Allow check.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// Allow atomically checks and, if permitted, records one request against
// key within the trailing window, admitting at most limit requests.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (Decision, error) {
	member, err := randomMember()
	if err != nil {
		return Decision{}, err
	}

	nowMs := time.Now().UnixMilli()
	windowMs := window.Milliseconds()

	res, err := l.client.Eval(ctx, slidingWindowScript, []string{key}, nowMs, windowMs, limit, member).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: eval sliding window script: %w", err)
	}

	values, ok := res.([]any)
	if !ok || len(values) != 2 {
		return Decision{}, fmt.Errorf("ratelimit: unexpected script result %v", res)
	}

	allowed, err := asInt64(values[0])
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: parse allowed: %w", err)
	}
	retryAfterMs, err := asInt64(values[1])
	if err != nil {
		return Decision{}, fmt.Errorf("ratelimit: parse retry-after: %w", err)
	}

	return Decision{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(retryAfterMs) * time.Millisecond,
	}, nil
}

func asInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	default:
		return 0, fmt.Errorf("ratelimit: value %v is not an integer", v)
	}
}

func randomMember() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("ratelimit: generate member nonce: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
