package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

// DeploymentEnvironment returns the environment ("" for the default) of the
// deployment with id, and whether there is one.
func (e *Environments) DeploymentEnvironment(ctx context.Context, id string) (string, bool, error) {
	var environment *string
	err := e.pool.QueryRow(ctx,
		`SELECT environment FROM integration_deployments WHERE id = $1::uuid`, id).Scan(&environment)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("environments repo: deployment environment: %w", err)
	}
	if environment == nil {
		return "", true, nil
	}
	return *environment, true, nil
}
