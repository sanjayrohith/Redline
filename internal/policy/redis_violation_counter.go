package policy

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisIncrExpirer is the subset of *redisclient.Client (which embeds
// *redis.Client) RedisViolationCounter needs.
type redisIncrExpirer interface {
	Incr(ctx context.Context, key string) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

// RedisViolationCounter implements ViolationCounter against a real Redis
// client.
type RedisViolationCounter struct {
	client redisIncrExpirer
}

// NewRedisViolationCounter returns a RedisViolationCounter backed by client.
func NewRedisViolationCounter(client redisIncrExpirer) *RedisViolationCounter {
	return &RedisViolationCounter{client: client}
}

// Incr implements ViolationCounter.
func (c *RedisViolationCounter) Incr(ctx context.Context, key string) (int64, error) {
	return c.client.Incr(ctx, key).Result()
}

// Expire implements ViolationCounter.
func (c *RedisViolationCounter) Expire(ctx context.Context, key string, ttl time.Duration) error {
	return c.client.Expire(ctx, key, ttl).Err()
}
