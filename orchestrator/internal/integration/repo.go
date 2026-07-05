package integration

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// selectColumns is the canonical projection (and order) that scanIntegration
// expects, kept in one place so every read stays in sync. It joins the creator
// and last-editor users (aliases cu/uu) to resolve display email/name; the base
// integration row is aliased i (real table or a CTE over a write).
const selectColumns = `i.id, i.name, i.definition, i.last_updated, i.created_by, i.updated_by,
	cu.email, cu.name, uu.email, uu.name`

// userJoins resolves the created_by/updated_by user ids to display fields. It
// requires the base row to be aliased i.
const userJoins = `LEFT JOIN users cu ON cu.id = i.created_by
	LEFT JOIN users uu ON uu.id = i.updated_by`

// writeReturning is the RETURNING list for an insert/update, exposing the base
// columns a following CTE join needs (aliased i in selectColumns).
const writeReturning = "id, name, definition, last_updated, created_by, updated_by"

// Repo persists integrations to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts a new integration and returns the stored row (with the actor
// resolved for display); id and last_updated are populated by the database.
// actorID is the creating user's id, or "" when unknown; it seeds both
// created_by and updated_by. The insert is wrapped in a CTE so the creator/editor
// can be joined for display in a single round trip.
func (r *Repo) Create(ctx context.Context, name, definition, actorID string) (Integration, error) {
	row := r.pool.QueryRow(ctx,
		`WITH i AS (
			INSERT INTO integrations (name, definition, created_by, updated_by)
			VALUES ($1, $2, NULLIF($3, '')::uuid, NULLIF($3, '')::uuid)
			RETURNING `+writeReturning+`
		)
		SELECT `+selectColumns+` FROM i `+userJoins,
		name, definition, actorID,
	)
	it, err := scanIntegration(row)
	if err != nil {
		return Integration{}, fmt.Errorf("integration repo: create: %w", err)
	}
	return it, nil
}

// NameExists reports whether another integration already uses name, compared
// case-insensitively. excludeID (a UUID in text form, or "" for a create) omits
// the row being renamed so an unchanged name doesn't collide with itself.
func (r *Repo) NameExists(ctx context.Context, name, excludeID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM integrations
			WHERE lower(name) = lower($1) AND ($2 = '' OR id::text <> $2)
		)`,
		name, excludeID,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("integration repo: name exists: %w", err)
	}
	return exists, nil
}

// Get returns the integration by id, or ErrNotFound if it does not exist.
func (r *Repo) Get(ctx context.Context, id string) (Integration, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+selectColumns+` FROM integrations i `+userJoins+` WHERE i.id = $1`, id,
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
		`SELECT `+selectColumns+` FROM integrations i `+userJoins+` ORDER BY i.name`,
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

// Update modifies name and definition, stamps last_updated and updated_by, and
// returns the updated row (actor resolved for display). created_by is left
// untouched. actorID is the editing user's id, or "" when unknown. Returns
// ErrNotFound if id does not exist.
func (r *Repo) Update(ctx context.Context, id, name, definition, actorID string) (Integration, error) {
	row := r.pool.QueryRow(ctx,
		`WITH i AS (
			UPDATE integrations
			SET name = $2, definition = $3, last_updated = now(), updated_by = NULLIF($4, '')::uuid
			WHERE id = $1
			RETURNING `+writeReturning+`
		)
		SELECT `+selectColumns+` FROM i `+userJoins,
		id, name, definition, actorID,
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

// scanIntegration reads one row in selectColumns order.
func scanIntegration(row pgx.Row) (Integration, error) {
	var it Integration
	if err := row.Scan(
		&it.ID, &it.Name, &it.Definition, &it.LastUpdated,
		&it.CreatedBy, &it.UpdatedBy,
		&it.CreatedByEmail, &it.CreatedByName,
		&it.UpdatedByEmail, &it.UpdatedByName,
	); err != nil {
		return Integration{}, err
	}
	return it, nil
}
