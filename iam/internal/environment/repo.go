// Package environment answers the one question iam has about environments:
// whether one exists. The orchestrator owns them — it creates their namespaces —
// and iam only refuses to bind a token or a key to a name that is not one, so a
// typo cannot mint a credential that would wake up the day somebody creates an
// environment by that name.
package environment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo reads the environments table the orchestrator writes.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Exists reports whether name is an environment. The default environment,
// spelled "", always is.
func (r *Repo) Exists(ctx context.Context, name string) (bool, error) {
	if name == "" {
		return true, nil
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM environments WHERE name = $1)`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("environment repo: exists: %w", err)
	}
	return exists, nil
}
