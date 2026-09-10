package snapshot

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

// frozenConfig aggregates a snapshot's frozen config files into two parallel
// arrays ordered by path, so scanSnapshot can fold them into the one definition
// a deploy still asks for. Correlated on s.id, which every query below exposes.
const frozenConfig = `
	(SELECT coalesce(array_agg(f.path ORDER BY f.path), '{}')
	   FROM integration_file_snapshots f
	  WHERE f.snapshot_id = s.id AND f.role = 'config'),
	(SELECT coalesce(array_agg(f.content ORDER BY f.path), '{}')
	   FROM integration_file_snapshots f
	  WHERE f.snapshot_id = s.id AND f.role = 'config')`

// snapshotColumns is the canonical column list (and order) that scanSnapshot
// expects, kept in one place so reads and RETURNING clauses stay in sync.
const snapshotColumns = "s.id, s.integration_id, s.tag, s.created_at," + frozenConfig

const (
	// pgUniqueViolation is raised when (integration_id, tag) already exists.
	pgUniqueViolation = "23505"
	// pgForeignKeyViolation is raised when integration_id references no integration.
	pgForeignKeyViolation = "23503"
)

// Repo persists snapshots to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Create tags integrationID and, in the same transaction, freezes a copy of every
// one of its files — flow files and resources alike — into
// integration_file_snapshots, so a deploy of this tag ships the definition and
// the resources that matched each other. A duplicate (integration_id, tag)
// surfaces as ErrTagExists; an unknown integration as ErrIntegrationNotFound.
//
// definition is no longer passed in and no longer stored. It used to be copied
// from a column while the resources were copied from a table, which left two
// mechanisms that could in principle disagree about when they ran. Now one
// INSERT ... SELECT freezes everything at one instant.
func (r *Repo) Create(ctx context.Context, integrationID, tag string) (Snapshot, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot repo: create: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx,
		`WITH s AS (
			INSERT INTO integration_snapshots (integration_id, tag)
			VALUES ($1, $2)
			RETURNING id, integration_id, tag, created_at
		)
		SELECT `+snapshotColumns+` FROM s`,
		integrationID, tag,
	)
	s, err := scanSnapshot(row)
	if err != nil {
		switch pgErrorCode(err) {
		case pgUniqueViolation:
			return Snapshot{}, ErrTagExists
		case pgForeignKeyViolation:
			return Snapshot{}, ErrIntegrationNotFound
		}
		return Snapshot{}, fmt.Errorf("snapshot repo: create: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO integration_file_snapshots (snapshot_id, kind, path, content, role)
		 SELECT $1, kind, path, content, role FROM integration_files WHERE integration_id = $2`,
		s.ID, integrationID,
	); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot repo: create: freeze files: %w", err)
	}

	// The files were frozen after the row above was read, so the projection could
	// not see them; read the definition back now that they are there.
	definition, err := frozenDefinition(ctx, tx, s.ID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snapshot repo: create: %w", err)
	}
	s.Definition = definition

	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot repo: create: commit: %w", err)
	}
	return s, nil
}

// frozenDefinition merges a snapshot's frozen config files.
func frozenDefinition(ctx context.Context, tx pgx.Tx, snapshotID string) (string, error) {
	rows, err := tx.Query(ctx,
		`SELECT path, content FROM integration_file_snapshots
		  WHERE snapshot_id = $1 AND role = $2
		  ORDER BY path`,
		snapshotID, string(projectfile.RoleConfig),
	)
	if err != nil {
		return "", err
	}
	files, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (projectfile.File, error) {
		var f projectfile.File
		err := row.Scan(&f.Path, &f.Content)
		return f, err
	})
	if err != nil {
		return "", err
	}
	return projectfile.Merge(files)
}

// Get returns the snapshot by id, or ErrNotFound if it does not exist.
func (r *Repo) Get(ctx context.Context, id string) (Snapshot, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+snapshotColumns+` FROM integration_snapshots s WHERE s.id = $1`, id,
	)
	s, err := scanSnapshot(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, ErrNotFound
		}
		return Snapshot{}, fmt.Errorf("snapshot repo: get: %w", err)
	}
	return s, nil
}

// ListByIntegration returns an integration's snapshots, newest first.
func (r *Repo) ListByIntegration(ctx context.Context, integrationID string) ([]Snapshot, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+snapshotColumns+`
		 FROM integration_snapshots s
		 WHERE s.integration_id = $1
		 ORDER BY s.created_at DESC`,
		integrationID,
	)
	if err != nil {
		return nil, fmt.Errorf("snapshot repo: list by integration: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Snapshot, error) {
		return scanSnapshot(row)
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot repo: list by integration: %w", err)
	}
	return items, nil
}

// Delete removes the snapshot. Returns ErrNotFound if no row was deleted.
func (r *Repo) Delete(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM integration_snapshots WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("snapshot repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeploymentsUsingSnapshot returns the human-facing labels (slug, falling back
// to name, then deployment id) of every deployment for integrationID that still
// references snapshotID. The snapshot id is recorded in two jsonb columns at
// deploy time — settings->>'snapshotId' and deployment_metadata->>'snapshotId' —
// so a match in either counts. Used to refuse deleting a tag that is live.
func (r *Repo) DeploymentsUsingSnapshot(ctx context.Context, integrationID, snapshotID string) ([]string, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT COALESCE(
			NULLIF(deployment_metadata->>'slug', ''),
			NULLIF(deployment_metadata->>'name', ''),
			id::text
		 )
		 FROM integration_deployments
		 WHERE integration_id = $1
		   AND $2 IN (settings->>'snapshotId', deployment_metadata->>'snapshotId')`,
		integrationID, snapshotID,
	)
	if err != nil {
		return nil, fmt.Errorf("snapshot repo: deployments using snapshot: %w", err)
	}
	labels, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("snapshot repo: deployments using snapshot: %w", err)
	}
	return labels, nil
}

// resourceColumns is the canonical column list (and order) that scanResource
// expects for a frozen resource. Frozen rows are written by Create (INSERT ...
// SELECT from integration_files) and are read-only thereafter.
const resourceColumns = "id, snapshot_id, kind, path, content, created_at"

// ListResources returns a snapshot's frozen resources ordered by name.
func (r *Repo) ListResources(ctx context.Context, snapshotID string) ([]Resource, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+resourceColumns+`
		 FROM integration_file_snapshots
		 WHERE snapshot_id = $1 AND role = $2
		 ORDER BY path`,
		snapshotID, string(projectfile.RoleResource),
	)
	if err != nil {
		return nil, fmt.Errorf("snapshot repo: list resources: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Resource, error) {
		return scanResource(row)
	})
	if err != nil {
		return nil, fmt.Errorf("snapshot repo: list resources: %w", err)
	}
	return items, nil
}

// ResourceContent returns the raw content of one frozen resource identified by
// its snapshot, kind and name. found is false (with a nil error) when no such
// resource exists — the caller maps that to a 404.
func (r *Repo) ResourceContent(ctx context.Context, snapshotID, kind, name string) ([]byte, bool, error) {
	var content string
	err := r.pool.QueryRow(ctx,
		`SELECT content FROM integration_file_snapshots
		 WHERE snapshot_id = $1 AND kind = $2 AND path = $3 AND role = $4`,
		snapshotID, kind, name, string(projectfile.RoleResource),
	).Scan(&content)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("snapshot repo: resource content: %w", err)
	}
	return []byte(content), true, nil
}

// scanSnapshot reads one row in snapshotColumns order.
func scanSnapshot(row pgx.Row) (Snapshot, error) {
	var (
		s        Snapshot
		paths    []string
		contents []string
	)
	if err := row.Scan(&s.ID, &s.IntegrationID, &s.Tag, &s.CreatedAt, &paths, &contents); err != nil {
		return Snapshot{}, err
	}
	if len(paths) != len(contents) {
		return Snapshot{}, fmt.Errorf("frozen config paths and contents disagree (%d vs %d)", len(paths), len(contents))
	}

	files := make([]projectfile.File, len(paths))
	for i := range paths {
		files[i] = projectfile.File{Path: paths[i], Content: contents[i]}
	}
	definition, err := projectfile.Merge(files)
	if err != nil {
		return Snapshot{}, err
	}
	s.Definition = definition
	return s, nil
}

// scanResource reads one frozen-resource row in resourceColumns order.
func scanResource(row pgx.Row) (Resource, error) {
	var res Resource
	if err := row.Scan(&res.ID, &res.SnapshotID, &res.Kind, &res.Name, &res.Content, &res.CreatedAt); err != nil {
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
