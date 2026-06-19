// Package db owns the orchestrator's Postgres connection pool. It keeps pool
// construction and teardown in one place so the rest of the service depends on
// a small typed handle rather than wiring pgxpool directly.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DB wraps the pgx connection pool used across the orchestrator.
type DB struct {
	pool *pgxpool.Pool
}

// New opens a connection pool for dsn and verifies it with a Ping so an
// unreachable database fails fast at startup rather than on the first query.
func New(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db: ping: %w", err)
	}

	return &DB{pool: pool}, nil
}

// Pool returns the underlying connection pool for use by repositories.
func (d *DB) Pool() *pgxpool.Pool {
	return d.pool
}

// Close releases the connection pool.
func (d *DB) Close() {
	d.pool.Close()
}
