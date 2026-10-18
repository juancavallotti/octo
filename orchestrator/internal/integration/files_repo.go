package integration

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/orchestrator/internal/projectfile"
)

// configRole is the role every file this file's queries touch carries, so a
// resource or a test suite sharing the table is never read or written as config.
const configRole = string(projectfile.RoleConfig)

// ConfigFiles returns an integration's config files ordered by path, or
// ErrNotFound when the integration does not exist. An integration with no config
// files yet returns an empty slice.
func (r *Repo) ConfigFiles(ctx context.Context, integrationID string) ([]ConfigFile, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT path, content, last_updated FROM integration_files
		  WHERE integration_id = $1 AND role = $2
		  ORDER BY path`,
		integrationID, configRole,
	)
	if err != nil {
		return nil, fmt.Errorf("integration repo: config files: %w", err)
	}
	ret, err := pgx.CollectRows(rows, pgx.RowToStructByPos[ConfigFile])
	if err != nil {
		return nil, fmt.Errorf("integration repo: config files: %w", err)
	}
	if len(ret) > 0 {
		return ret, nil
	}
	// No rows is either an empty project or no project at all, and only the
	// second is an error.
	if err := r.ensureExists(ctx, integrationID); err != nil {
		return nil, err
	}
	return []ConfigFile{}, nil
}

// PutConfigFile writes one config file, creating it when path is new, and stamps
// the integration as edited by actorID ("" when unknown). Returns ErrNotFound
// when the integration does not exist.
//
// check, when not nil, is handed the integration's config files as they stand
// before the write and refuses it by returning an error. It runs inside the write's
// transaction, after the integration's row is locked, so it judges the files the
// write actually lands on: a concurrent write to another file waits rather than
// slipping in between the check and the write.
func (r *Repo) PutConfigFile(
	ctx context.Context, integrationID, path, content, actorID string, check func([]ConfigFile) error,
) (ConfigFile, error) {
	ret, err := inTx(ctx, r.pool, func(tx pgx.Tx) (ConfigFile, error) {
		if err := touch(ctx, tx, integrationID, actorID); err != nil {
			return ConfigFile{}, err
		}
		if check != nil {
			current, err := configFilesIn(ctx, tx, integrationID)
			if err != nil {
				return ConfigFile{}, err
			}
			if err := check(current); err != nil {
				return ConfigFile{}, err
			}
		}
		return upsertConfigFile(ctx, tx, integrationID, path, content, actorID)
	})
	if err != nil {
		return ConfigFile{}, wrapFileErr("put config file", err)
	}
	return ret, nil
}

// ReplaceConfigFiles makes files the integration's whole set of config files in
// one transaction, deleting every config file not among them, and stamps the
// integration as edited by actorID. Returns ErrNotFound for an unknown integration.
func (r *Repo) ReplaceConfigFiles(ctx context.Context, integrationID string, files []projectfile.File, actorID string) error {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	_, err := inTx(ctx, r.pool, func(tx pgx.Tx) (struct{}, error) {
		if err := touch(ctx, tx, integrationID, actorID); err != nil {
			return struct{}{}, err
		}
		if _, err := tx.Exec(ctx,
			`DELETE FROM integration_files
			  WHERE integration_id = $1 AND role = $2 AND NOT (path = ANY($3))`,
			integrationID, configRole, paths,
		); err != nil {
			return struct{}{}, err
		}
		for _, f := range files {
			if _, err := upsertConfigFile(ctx, tx, integrationID, f.Path, f.Content, actorID); err != nil {
				return struct{}{}, err
			}
		}
		return struct{}{}, nil
	})
	return wrapFileErr("replace config files", err)
}

// DeleteConfigFile removes one config file and stamps the integration as edited
// by actorID. Returns ErrNotFound for an unknown integration and ErrFileNotFound
// when it has no config file at path.
func (r *Repo) DeleteConfigFile(ctx context.Context, integrationID, path, actorID string) error {
	_, err := inTx(ctx, r.pool, func(tx pgx.Tx) (struct{}, error) {
		if err := touch(ctx, tx, integrationID, actorID); err != nil {
			return struct{}{}, err
		}
		tag, err := tx.Exec(ctx,
			`DELETE FROM integration_files
			  WHERE integration_id = $1 AND path = $2 AND role = $3`,
			integrationID, path, configRole,
		)
		if err != nil {
			return struct{}{}, err
		}
		if tag.RowsAffected() == 0 {
			return struct{}{}, fmt.Errorf("%w: %q", ErrFileNotFound, path)
		}
		return struct{}{}, nil
	})
	return wrapFileErr("delete config file", err)
}

// ensureExists returns ErrNotFound unless the integration exists.
func (r *Repo) ensureExists(ctx context.Context, integrationID string) error {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM integrations WHERE id = $1)`, integrationID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("integration repo: exists: %w", err)
	}
	if !exists {
		return ErrNotFound
	}
	return nil
}

// touch stamps an integration as edited by actorID: writing a file is editing the
// integration it belongs to.
func touch(ctx context.Context, tx pgx.Tx, integrationID, actorID string) error {
	tag, err := tx.Exec(ctx,
		`UPDATE integrations SET last_updated = now(), updated_by = NULLIF($2, '')::uuid
		  WHERE id = $1`,
		integrationID, actorID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// upsertConfigFile writes content at path, attributing a new file's creation and
// every edit to actorID.
func upsertConfigFile(ctx context.Context, tx pgx.Tx, integrationID, path, content, actorID string) (ConfigFile, error) {
	row := tx.QueryRow(ctx,
		`INSERT INTO integration_files (integration_id, path, role, kind, content, created_by, updated_by)
		 VALUES ($1, $2, $3, '', $4, NULLIF($5, '')::uuid, NULLIF($5, '')::uuid)
		 ON CONFLICT (integration_id, path)
		 DO UPDATE SET content = EXCLUDED.content, updated_by = EXCLUDED.updated_by, last_updated = now()
		 RETURNING path, content, last_updated`,
		integrationID, path, string(projectfile.Classify(path)), content, actorID,
	)
	var ret ConfigFile
	if err := row.Scan(&ret.Path, &ret.Content, &ret.LastUpdated); err != nil {
		return ConfigFile{}, err
	}
	return ret, nil
}

// configFilesIn reads an integration's config files ordered by path, inside tx.
func configFilesIn(ctx context.Context, tx pgx.Tx, integrationID string) ([]ConfigFile, error) {
	rows, err := tx.Query(ctx,
		`SELECT path, content, last_updated FROM integration_files
		  WHERE integration_id = $1 AND role = $2
		  ORDER BY path`,
		integrationID, configRole,
	)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ConfigFile])
}

// wrapFileErr adds the operation to an unexpected error, passing the domain
// errors through untouched so callers can match on them.
func wrapFileErr(op string, err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrFileNotFound) ||
		errors.Is(err, ErrFilesConflict) {
		return err
	}
	return fmt.Errorf("integration repo: %s: %w", op, err)
}

// inTx runs fn inside a transaction, rolling back on any error.
func inTx[T any](ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := pool.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	ret, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return ret, nil
}
