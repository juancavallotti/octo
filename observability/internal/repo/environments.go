package repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Environments answers whether a name is an environment. The orchestrator owns
// the table; this service only refuses a request that names one that is not.
type Environments struct {
	pool *pgxpool.Pool
}

// NewEnvironments returns an Environments backed by pool.
func NewEnvironments(pool *pgxpool.Pool) *Environments {
	return &Environments{pool: pool}
}

// Exists reports whether name is an environment. The default, "", always is.
func (e *Environments) Exists(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return true, nil
	}
	var exists bool
	if err := e.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM environments WHERE name = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("environments repo: exists: %w", err)
	}
	return exists, nil
}
