package integration

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// integrationColumns is the canonical column list (and order) that
// scanIntegration expects, kept in one place so reads and RETURNING clauses
// stay in sync.
const integrationColumns = "id, name, definition, last_updated"

// Repo persists integrations to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts a new integration and returns the stored row; id and
// last_updated are populated by the database via RETURNING.
func (r *Repo) Create(ctx context.Context, name, definition string) (Integration, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO integrations (name, definition)
		 VALUES ($1, $2)
		 RETURNING `+integrationColumns,
		name, definition,
	)
	it, err := scanIntegration(row)
	if err != nil {
		return Integration{}, fmt.Errorf("integration repo: create: %w", err)
	}
	return it, nil
}

// Get returns the integration by id, or ErrNotFound if it does not exist.
func (r *Repo) Get(ctx context.Context, id string) (Integration, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+integrationColumns+` FROM integrations WHERE id = $1`, id,
	)
	it, err := scanIntegration(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Integration{}, ErrNotFound
		}
		return Integration{}, fmt.Errorf("integration repo: get: %w", err)
	}
	return it, nil
}

// List returns all integrations ordered by name. (Pagination is deferred.)
func (r *Repo) List(ctx context.Context) ([]Integration, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+integrationColumns+` FROM integrations ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("integration repo: list: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Integration, error) {
		return scanIntegration(row)
	})
	if err != nil {
		return nil, fmt.Errorf("integration repo: list: %w", err)
	}
	return items, nil
}

// Update modifies name and definition, stamps last_updated, and returns the
// updated row. Returns ErrNotFound if id does not exist.
func (r *Repo) Update(ctx context.Context, id, name, definition string) (Integration, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE integrations
		 SET name = $2, definition = $3, last_updated = now()
		 WHERE id = $1
		 RETURNING `+integrationColumns,
		id, name, definition,
	)
	it, err := scanIntegration(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Integration{}, ErrNotFound
		}
		return Integration{}, fmt.Errorf("integration repo: update: %w", err)
	}
	return it, nil
}

// Delete removes the integration. Returns ErrNotFound if no row was deleted.
func (r *Repo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM integrations WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("integration repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanIntegration reads one row in integrationColumns order.
func scanIntegration(row pgx.Row) (Integration, error) {
	var it Integration
	if err := row.Scan(&it.ID, &it.Name, &it.Definition, &it.LastUpdated); err != nil {
		return Integration{}, err
	}
	return it, nil
}
