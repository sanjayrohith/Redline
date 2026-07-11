package policy

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisSetterExister is the subset of *redisclient.Client (which embeds
// *redis.Client) RateReductionStore needs.
type redisSetterExister interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Exists(ctx context.Context, keys ...string) *redis.IntCmd
}

// RateReductionStore durably records which accounts currently have a
// reduced effective rate limit, for AbuseGuardMiddleware to enforce on
// every request without re-running the escalation decision each time.
type RateReductionStore struct {
	client redisSetterExister
}

// NewRateReductionStore returns a RateReductionStore backed by client.
func NewRateReductionStore(client redisSetterExister) *RateReductionStore {
	return &RateReductionStore{client: client}
}

// Reduce marks userID reduced for ttl. Implements RateReducer.
func (s *RateReductionStore) Reduce(ctx context.Context, userID string, ttl time.Duration) error {
	return s.client.Set(ctx, reducedKey(userID), "1", ttl).Err()
}

// IsReduced reports whether userID is currently marked reduced.
func (s *RateReductionStore) IsReduced(ctx context.Context, userID string) (bool, error) {
	n, err := s.client.Exists(ctx, reducedKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func reducedKey(userID string) string { return "abuse:reduced:" + userID }
