// Package db wires the PostgreSQL connection pool and its health probe.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps a pgxpool.Pool so callers depend on this package's type
// rather than importing pgxpool directly.
type Pool struct {
	*pgxpool.Pool
}

// NewPool parses dsn and constructs a connection pool configured with
// maxConns and connectTimeout. Connections are established lazily; use
// HealthCheck to verify reachability.
func NewPool(ctx context.Context, dsn string, maxConns int32, connectTimeout time.Duration) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: parse dsn: %w", err)
	}

	cfg.MaxConns = maxConns
	cfg.ConnConfig.ConnectTimeout = connectTimeout

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	return &Pool{Pool: pool}, nil
}

// HealthCheck pings the database, bounding the attempt by ctx.
func (p *Pool) HealthCheck(ctx context.Context) error {
	if err := p.Ping(ctx); err != nil {
		return fmt.Errorf("db: ping: %w", err)
	}
	return nil
}
