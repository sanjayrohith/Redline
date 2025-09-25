// Package redisclient wires the gateway's Redis connection pool and its
// health probe.
package redisclient

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Options configures a new Client.
type Options struct {
	Addr        string
	Password    string
	DB          int
	PoolSize    int
	MaxRetries  int
	DialTimeout time.Duration
}

// Client wraps a redis.Client so callers depend on this package's type
// rather than importing go-redis directly.
type Client struct {
	*redis.Client
}

// NewClient constructs a Redis client from opts. Like the Postgres pool,
// connections are established lazily; use HealthCheck to verify reachability.
func NewClient(opts Options) *Client {
	return &Client{
		Client: redis.NewClient(&redis.Options{
			Addr:        opts.Addr,
			Password:    opts.Password,
			DB:          opts.DB,
			PoolSize:    opts.PoolSize,
			MaxRetries:  opts.MaxRetries,
			DialTimeout: opts.DialTimeout,
		}),
	}
}

// HealthCheck pings Redis, bounding the attempt by ctx.
func (c *Client) HealthCheck(ctx context.Context) error {
	if err := c.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis: ping: %w", err)
	}
	return nil
}
