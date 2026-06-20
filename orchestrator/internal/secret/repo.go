package secret

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// secretColumns is the canonical column list (and order) scanSecret expects.
const secretColumns = "name, created_at, last_updated"

// Repo persists the cluster-secret catalog to Postgres. It stores names and
// timestamps only — never values.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Upsert records name in the catalog, creating it or bumping last_updated on an
// existing row, and returns the resulting entry.
func (r *Repo) Upsert(ctx context.Context, name string) (Secret, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO cluster_secrets (name) VALUES ($1)
		 ON CONFLICT (name) DO UPDATE SET last_updated = now()
		 RETURNING `+secretColumns,
		name,
	)
	s, err := scanSecret(row)
	if err != nil {
		return Secret{}, fmt.Errorf("secret repo: upsert: %w", err)
	}
	return s, nil
}

// List returns all catalog entries, ordered by name.
func (r *Repo) List(ctx context.Context) ([]Secret, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+secretColumns+` FROM cluster_secrets ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("secret repo: list: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Secret, error) {
		return scanSecret(row)
	})
	if err != nil {
		return nil, fmt.Errorf("secret repo: list: %w", err)
	}
	return items, nil
}

// Delete removes name from the catalog. Returns ErrNotFound if no row was deleted.
func (r *Repo) Delete(ctx context.Context, name string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM cluster_secrets WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("secret repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanSecret reads one row in secretColumns order.
func scanSecret(row pgx.Row) (Secret, error) {
	var s Secret
	if err := row.Scan(&s.Name, &s.CreatedAt, &s.LastUpdated); err != nil {
		return Secret{}, err
	}
	return s, nil
}
