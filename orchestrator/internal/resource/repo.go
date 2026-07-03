package resource

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// resourceColumns is the canonical column list (and order) that scanResource
// expects, kept in one place so reads and RETURNING clauses stay in sync.
const resourceColumns = "id, integration_id, kind, name, content, created_at, last_updated"

const (
	// pgUniqueViolation is raised when (integration_id, name) already exists.
	pgUniqueViolation = "23505"
	// pgForeignKeyViolation is raised when integration_id references no integration.
	pgForeignKeyViolation = "23503"
)

// Repo persists resources to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts a new resource under integrationID and returns the stored row.
// A duplicate (integration_id, name) surfaces as ErrNameExists; an unknown
// integration as ErrIntegrationNotFound.
func (r *Repo) Create(ctx context.Context, integrationID, kind, name, content string) (Resource, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO integration_resources (integration_id, kind, name, content)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+resourceColumns,
		integrationID, kind, name, content,
	)
	res, err := scanResource(row)
	if err != nil {
		switch pgErrorCode(err) {
		case pgUniqueViolation:
			return Resource{}, ErrNameExists
		case pgForeignKeyViolation:
			return Resource{}, ErrIntegrationNotFound
		}
		return Resource{}, fmt.Errorf("resource repo: create: %w", err)
	}
	return res, nil
}

// Get returns the resource by id within integrationID, or ErrNotFound if no such
// resource belongs to that integration. Scoping the query by integration_id
// makes the nested route authoritative rather than decorative.
func (r *Repo) Get(ctx context.Context, integrationID, id string) (Resource, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+resourceColumns+`
		 FROM integration_resources WHERE id = $1 AND integration_id = $2`,
		id, integrationID,
	)
	res, err := scanResource(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resource{}, ErrNotFound
		}
		return Resource{}, fmt.Errorf("resource repo: get: %w", err)
	}
	return res, nil
}

// ListByIntegration returns an integration's resources ordered by name.
func (r *Repo) ListByIntegration(ctx context.Context, integrationID string) ([]Resource, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+resourceColumns+`
		 FROM integration_resources
		 WHERE integration_id = $1
		 ORDER BY name`,
		integrationID,
	)
	if err != nil {
		return nil, fmt.Errorf("resource repo: list by integration: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Resource, error) {
		return scanResource(row)
	})
	if err != nil {
		return nil, fmt.Errorf("resource repo: list by integration: %w", err)
	}
	return items, nil
}

// Update modifies kind, name and content, stamps last_updated, and returns the
// updated row. It matches on both id and integration_id so a resource is only
// mutated within its own integration; ErrNotFound if no such row. A rename that
// collides with an existing name surfaces as ErrNameExists.
func (r *Repo) Update(ctx context.Context, integrationID, id, kind, name, content string) (Resource, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE integration_resources
		 SET kind = $3, name = $4, content = $5, last_updated = now()
		 WHERE id = $1 AND integration_id = $2
		 RETURNING `+resourceColumns,
		id, integrationID, kind, name, content,
	)
	res, err := scanResource(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resource{}, ErrNotFound
		}
		if pgErrorCode(err) == pgUniqueViolation {
			return Resource{}, ErrNameExists
		}
		return Resource{}, fmt.Errorf("resource repo: update: %w", err)
	}
	return res, nil
}

// Delete removes the resource within integrationID. Returns ErrNotFound if no
// matching row was deleted.
func (r *Repo) Delete(ctx context.Context, integrationID, id string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM integration_resources WHERE id = $1 AND integration_id = $2`,
		id, integrationID,
	)
	if err != nil {
		return fmt.Errorf("resource repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanResource reads one row in resourceColumns order.
func scanResource(row pgx.Row) (Resource, error) {
	var res Resource
	if err := row.Scan(
		&res.ID, &res.IntegrationID, &res.Kind, &res.Name, &res.Content,
		&res.CreatedAt, &res.LastUpdated,
	); err != nil {
		return Resource{}, err
	}
	return res, nil
}

// pgErrorCode returns the SQLSTATE code of a Postgres error, or "" if err is not
// a *pgconn.PgError.
func pgErrorCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
