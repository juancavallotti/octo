package folder

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/eip-go/orchestrator/internal/integration"
)

// folderColumns is the canonical column list (and order) that scanFolder expects,
// kept in one place so reads and RETURNING clauses stay in sync.
const folderColumns = "id, parent_id, name"

// pgForeignKeyViolation is the SQLSTATE Postgres raises when a membership row
// references a folder or integration that does not exist.
const pgForeignKeyViolation = "23503"

// Repo persists folders and their integration membership to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create inserts a new folder under parentID (nil for a root) and returns the
// stored row; id is populated by the database via RETURNING.
func (r *Repo) Create(ctx context.Context, name string, parentID *string) (Folder, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO integration_idx_structure (parent_id, name)
		 VALUES ($1, $2)
		 RETURNING `+folderColumns,
		parentID, name,
	)
	f, err := scanFolder(row)
	if err != nil {
		if isForeignKeyViolation(err) {
			return Folder{}, ErrNotFound
		}
		return Folder{}, fmt.Errorf("folder repo: create: %w", err)
	}
	return f, nil
}

// Get returns the folder by id, or ErrNotFound if it does not exist.
func (r *Repo) Get(ctx context.Context, id string) (Folder, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+folderColumns+` FROM integration_idx_structure WHERE id = $1`, id,
	)
	f, err := scanFolder(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Folder{}, ErrNotFound
		}
		return Folder{}, fmt.Errorf("folder repo: get: %w", err)
	}
	return f, nil
}

// List returns every folder ordered by name. The flat result is assembled into a
// tree by the service.
func (r *Repo) List(ctx context.Context) ([]Folder, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+folderColumns+` FROM integration_idx_structure ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("folder repo: list: %w", err)
	}
	folders, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Folder, error) {
		return scanFolder(row)
	})
	if err != nil {
		return nil, fmt.Errorf("folder repo: list: %w", err)
	}
	return folders, nil
}

// Update renames the folder and reparents it (parentID nil moves it to a root),
// returning the updated row. Returns ErrNotFound if id does not exist.
func (r *Repo) Update(ctx context.Context, id, name string, parentID *string) (Folder, error) {
	row := r.pool.QueryRow(ctx,
		`UPDATE integration_idx_structure
		 SET name = $2, parent_id = $3
		 WHERE id = $1
		 RETURNING `+folderColumns,
		id, name, parentID,
	)
	f, err := scanFolder(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Folder{}, ErrNotFound
		}
		if isForeignKeyViolation(err) {
			return Folder{}, ErrNotFound
		}
		return Folder{}, fmt.Errorf("folder repo: update: %w", err)
	}
	return f, nil
}

// Delete removes the folder; the schema cascades to child folders and membership
// rows. Returns ErrNotFound if no row was deleted.
func (r *Repo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM integration_idx_structure WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("folder repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddIntegration places integrationID in folderID. Membership is single-folder,
// so this moves the integration if it already belonged elsewhere. A missing
// folder or integration surfaces as ErrNotFound.
func (r *Repo) AddIntegration(ctx context.Context, folderID, integrationID string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO integration_folder_members (integration_id, folder_id)
		 VALUES ($1, $2)
		 ON CONFLICT (integration_id) DO UPDATE SET folder_id = EXCLUDED.folder_id`,
		integrationID, folderID,
	)
	if err != nil {
		if isForeignKeyViolation(err) {
			return ErrNotFound
		}
		return fmt.Errorf("folder repo: add integration: %w", err)
	}
	return nil
}

// RemoveIntegration removes integrationID from folderID. Returns ErrNotFound if
// no such membership exists.
func (r *Repo) RemoveIntegration(ctx context.Context, folderID, integrationID string) error {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM integration_folder_members WHERE folder_id = $1 AND integration_id = $2`,
		folderID, integrationID,
	)
	if err != nil {
		return fmt.Errorf("folder repo: remove integration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListIntegrations returns the integrations that belong to folderID, ordered by
// name. An empty result is returned for an unknown or empty folder.
func (r *Repo) ListIntegrations(ctx context.Context, folderID string) ([]integration.Integration, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT i.id, i.name, i.definition, i.last_updated
		 FROM integrations i
		 JOIN integration_folder_members m ON m.integration_id = i.id
		 WHERE m.folder_id = $1
		 ORDER BY i.name`,
		folderID,
	)
	if err != nil {
		return nil, fmt.Errorf("folder repo: list integrations: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (integration.Integration, error) {
		var it integration.Integration
		if scanErr := row.Scan(&it.ID, &it.Name, &it.Definition, &it.LastUpdated); scanErr != nil {
			return integration.Integration{}, scanErr
		}
		return it, nil
	})
	if err != nil {
		return nil, fmt.Errorf("folder repo: list integrations: %w", err)
	}
	return items, nil
}

// scanFolder reads one row in folderColumns order. parent_id is NULL for roots,
// scanned into a nil *string.
func scanFolder(row pgx.Row) (Folder, error) {
	var f Folder
	if err := row.Scan(&f.ID, &f.ParentID, &f.Name); err != nil {
		return Folder{}, err
	}
	return f, nil
}

// isForeignKeyViolation reports whether err is a Postgres foreign-key violation,
// which for membership writes means a referenced folder or integration is gone.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgForeignKeyViolation
}
