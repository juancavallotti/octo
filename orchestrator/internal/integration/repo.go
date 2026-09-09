package integration

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

// defaultConfigPath is where a new integration's definition is written. It
// matches the name the bundle format gives the definition at the root of an
// archive, so exporting and unzipping a fresh integration produces the file it
// already had.
const defaultConfigPath = "integration.yaml"

// configFiles aggregates an integration's config files into two parallel arrays,
// ordered by path so the two line up and so the merge sees files in the same
// order a directory load would. Correlated on i.id, which every query below
// exposes — including the CTEs, where i is the written row.
const configFiles = `
	(SELECT coalesce(array_agg(f.path ORDER BY f.path), '{}')
	   FROM integration_files f
	  WHERE f.integration_id = i.id AND f.role = 'config'),
	(SELECT coalesce(array_agg(f.content ORDER BY f.path), '{}')
	   FROM integration_files f
	  WHERE f.integration_id = i.id AND f.role = 'config')`

// selectColumns is the canonical projection (and order) that scanIntegration
// expects, kept in one place so every read stays in sync. It joins the creator
// and last-editor users (aliases cu/uu) to resolve display email/name; the base
// integration row is aliased i (real table or a CTE over a write).
const selectColumns = `i.id, i.name, i.icon, i.last_updated, i.created_by, i.updated_by,
	cu.email, cu.name, uu.email, uu.name,` + configFiles

// userJoins resolves the created_by/updated_by user ids to display fields. It
// requires the base row to be aliased i.
const userJoins = `LEFT JOIN users cu ON cu.id = i.created_by
	LEFT JOIN users uu ON uu.id = i.updated_by`

// writeReturning is the RETURNING list for an insert/update, exposing the base
// columns a following CTE join needs (aliased i in selectColumns).
const writeReturning = "id, name, icon, last_updated, created_by, updated_by"

// Repo persists integrations to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts a new integration and its definition, and returns the stored
// row (with the actor resolved for display); id and last_updated are populated
// by the database. actorID is the creating user's id, or "" when unknown; it
// seeds both created_by and updated_by.
//
// The row and its config file are written in one transaction: an integration
// that exists without the definition it was created with is not a state anyone
// should have to handle.
func (r *Repo) Create(ctx context.Context, name, definition, actorID string) (Integration, error) {
	it, err := r.inTx(ctx, func(tx pgx.Tx) (Integration, error) {
		row := tx.QueryRow(ctx,
			`WITH i AS (
				INSERT INTO integrations (name, created_by, updated_by)
				VALUES ($1, NULLIF($2, '')::uuid, NULLIF($2, '')::uuid)
				RETURNING `+writeReturning+`
			)
			SELECT `+selectColumns+` FROM i `+userJoins,
			name, actorID,
		)
		created, scanErr := scanIntegration(row)
		if scanErr != nil {
			return Integration{}, scanErr
		}
		if definition != "" {
			if err := writeConfigFile(ctx, tx, created.ID, defaultConfigPath, definition); err != nil {
				return Integration{}, err
			}
		}
		// The file was written after the row was read, so the projection above
		// could not see it. We know what we wrote, and one file merges to itself.
		created.Definition = definition
		return created, nil
	})
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
//
// The definition is written to the integration's config file. A project with
// several config files has no single file this definition belongs to, so it is
// refused rather than guessed at — see ErrAmbiguousDefinition.
func (r *Repo) Update(ctx context.Context, id, name, definition, actorID string) (Integration, error) {
	it, err := r.inTx(ctx, func(tx pgx.Tx) (Integration, error) {
		row := tx.QueryRow(ctx,
			`WITH i AS (
				UPDATE integrations
				SET name = $2, last_updated = now(), updated_by = NULLIF($3, '')::uuid
				WHERE id = $1
				RETURNING `+writeReturning+`
			)
			SELECT `+selectColumns+` FROM i `+userJoins,
			id, name, actorID,
		)
		updated, scanErr := scanIntegration(row)
		if scanErr != nil {
			return Integration{}, scanErr
		}

		path, pathErr := configPathFor(ctx, tx, id)
		if pathErr != nil {
			return Integration{}, pathErr
		}
		if err := writeConfigFile(ctx, tx, id, path, definition); err != nil {
			return Integration{}, err
		}
		updated.Definition = definition
		return updated, nil
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Integration{}, ErrNotFound
		}
		return Integration{}, fmt.Errorf("integration repo: update: %w", err)
	}
	return it, nil
}

// SetIcon records an integration's chosen icon, or clears it back to derived
// when icon is "". It is its own write rather than a field on Update because
// every caller of Update passes a whole integration — the editor's save, a
// bundle replace, the agent's republish — and none of them know about icons. A
// field there would have them clear a user's choice each time they saved
// something unrelated.
func (r *Repo) SetIcon(ctx context.Context, id, icon, actorID string) (Integration, error) {
	row := r.pool.QueryRow(ctx,
		`WITH i AS (
			UPDATE integrations
			SET icon = $2, last_updated = now(), updated_by = NULLIF($3, '')::uuid
			WHERE id = $1
			RETURNING `+writeReturning+`
		)
		SELECT `+selectColumns+` FROM i `+userJoins,
		id, icon, actorID,
	)
	it, err := scanIntegration(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Integration{}, ErrNotFound
		}
		return Integration{}, fmt.Errorf("integration repo: set icon: %w", err)
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

// inTx runs fn inside a transaction, rolling back on any error.
func (r *Repo) inTx(ctx context.Context, fn func(pgx.Tx) (Integration, error)) (Integration, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Integration{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	it, err := fn(tx)
	if err != nil {
		return Integration{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Integration{}, err
	}
	return it, nil
}

// configPathFor picks the file an integration's definition is written to: the
// one config file it has, or the default name when it has none yet.
//
// Several config files means the caller handed us a whole definition with no way
// to say which file it replaces, and collapsing the project to answer that would
// destroy the others. It is refused instead.
func configPathFor(ctx context.Context, tx pgx.Tx, integrationID string) (string, error) {
	rows, err := tx.Query(ctx,
		`SELECT path FROM integration_files
		  WHERE integration_id = $1 AND role = $2
		  ORDER BY path`,
		integrationID, string(projectfile.RoleConfig),
	)
	if err != nil {
		return "", err
	}
	paths, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}

	switch len(paths) {
	case 0:
		return defaultConfigPath, nil
	case 1:
		return paths[0], nil
	default:
		return "", fmt.Errorf("%w: %v", ErrAmbiguousDefinition, paths)
	}
}

// writeConfigFile upserts an integration's definition at path.
func writeConfigFile(ctx context.Context, tx pgx.Tx, integrationID, path, definition string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO integration_files (integration_id, path, role, kind, content)
		 VALUES ($1, $2, $3, '', $4)
		 ON CONFLICT (integration_id, path)
		 DO UPDATE SET content = EXCLUDED.content, last_updated = now()`,
		integrationID, path, string(projectfile.Classify(path)), definition,
	)
	return err
}

// scanIntegration reads one row in selectColumns order, folding the config files
// into the single definition the rest of the orchestrator speaks in.
func scanIntegration(row pgx.Row) (Integration, error) {
	var (
		it       Integration
		paths    []string
		contents []string
	)
	if err := row.Scan(
		&it.ID, &it.Name, &it.Icon, &it.LastUpdated,
		&it.CreatedBy, &it.UpdatedBy,
		&it.CreatedByEmail, &it.CreatedByName,
		&it.UpdatedByEmail, &it.UpdatedByName,
		&paths, &contents,
	); err != nil {
		return Integration{}, err
	}

	definition, err := mergeConfig(paths, contents)
	if err != nil {
		return Integration{}, err
	}
	it.Definition = definition
	return it, nil
}

// mergeConfig folds the parallel path/content arrays into one definition.
func mergeConfig(paths, contents []string) (string, error) {
	if len(paths) != len(contents) {
		return "", fmt.Errorf("config file paths and contents disagree (%d vs %d)", len(paths), len(contents))
	}
	files := make([]projectfile.File, len(paths))
	for i := range paths {
		files[i] = projectfile.File{Path: paths[i], Content: contents[i]}
	}
	return projectfile.Merge(files)
}
