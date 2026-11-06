package environment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// environmentColumns is the canonical column list (and order) scanEnvironment expects.
const environmentColumns = "name, namespace, display_name, description, created_at, created_by::text"

// uniqueViolation and foreignKeyViolation are Postgres's SQLSTATEs for the two
// constraints these writes can trip.
const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
)

// Repo persists environments to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts e. A name or namespace that is already taken comes back as
// ErrExists or ErrNamespaceTaken, decided by the constraint that refused it, so
// a concurrent create that slipped past the service's checks is still reported
// as what it is.
func (r *Repo) Create(ctx context.Context, e Environment) (Environment, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO environments (name, namespace, display_name, description, created_by)
		 VALUES ($1, $2, $3, $4, NULLIF($5, '')::uuid)
		 RETURNING `+environmentColumns,
		e.Name, e.Namespace, e.DisplayName, e.Description, deref(e.CreatedBy),
	)
	out, err := scanEnvironment(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			if pgErr.ConstraintName == "environments_namespace_key" {
				return Environment{}, ErrNamespaceTaken
			}
			return Environment{}, ErrExists
		}
		return Environment{}, fmt.Errorf("environment repo: create: %w", err)
	}
	return out, nil
}

// Get returns the environment named name, or ErrNotFound.
func (r *Repo) Get(ctx context.Context, name string) (Environment, error) {
	row := r.pool.QueryRow(ctx, `SELECT `+environmentColumns+` FROM environments WHERE name = $1`, name)
	e, err := scanEnvironment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Environment{}, ErrNotFound
		}
		return Environment{}, fmt.Errorf("environment repo: get: %w", err)
	}
	return e, nil
}

// List returns every stored environment, ordered by name.
func (r *Repo) List(ctx context.Context) ([]Environment, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+environmentColumns+` FROM environments ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("environment repo: list: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Environment, error) {
		return scanEnvironment(row)
	})
	if err != nil {
		return nil, fmt.Errorf("environment repo: list: %w", err)
	}
	return items, nil
}

// Update applies p to the environment named name and returns the result. A nil
// field is left as it is.
func (r *Repo) Update(ctx context.Context, name string, p Patch) (Environment, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE environments
		    SET display_name = COALESCE($2, display_name),
		        description  = COALESCE($3, description)
		  WHERE name = $1
		 RETURNING `+environmentColumns,
		name, p.DisplayName, p.Description,
	)
	e, err := scanEnvironment(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Environment{}, ErrNotFound
		}
		return Environment{}, fmt.Errorf("environment repo: update: %w", err)
	}
	return e, nil
}

// HasIntegrations reports whether any integration still belongs to the
// environment named name. Deployments belong to an integration, so this covers
// them too.
func (r *Repo) HasIntegrations(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM integrations WHERE environment = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("environment repo: has integrations: %w", err)
	}
	return exists, nil
}

// Delete removes the environment named name. Returns ErrNotFound if no row was deleted.
func (r *Repo) Delete(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM environments WHERE name = $1`, name)
	if err != nil {
		// The foreign keys from integrations and deployments, the backstop to
		// HasIntegrations against one created between the check and this delete.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == foreignKeyViolation {
			return ErrInUse
		}
		return fmt.Errorf("environment repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanEnvironment reads one row in environmentColumns order.
func scanEnvironment(row pgx.Row) (Environment, error) {
	var e Environment
	if err := row.Scan(&e.Name, &e.Namespace, &e.DisplayName, &e.Description, &e.CreatedAt, &e.CreatedBy); err != nil {
		return Environment{}, err
	}
	return e, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ownerQueries reads the environment of one row of each kind current.Confine
// asks about. A snapshot belongs to its integration's environment.
var ownerQueries = map[string]string{
	"integrations": `SELECT environment FROM integrations WHERE id = $1`,
	"deployments":  `SELECT environment FROM integration_deployments WHERE id = $1`,
	"folders":      `SELECT environment FROM integration_idx_structure WHERE id = $1`,
	"snapshots": `SELECT i.environment FROM integration_snapshots s
	              JOIN integrations i ON i.id = s.integration_id WHERE s.id = $1`,
}

// EnvironmentOf returns the environment ("" for the default) of the row of kind
// with id, and whether there is one.
func (r *Repo) EnvironmentOf(ctx context.Context, kind, id string) (string, bool, error) {
	query, ok := ownerQueries[kind]
	if !ok {
		return "", false, fmt.Errorf("environment repo: no owner query for %q", kind)
	}
	var environment *string
	if err := r.pool.QueryRow(ctx, query, id).Scan(&environment); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("environment repo: environment of %s: %w", kind, err)
	}
	return deref(environment), true, nil
}
