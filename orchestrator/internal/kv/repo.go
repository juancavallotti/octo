package kv

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgUniqueViolation is the SQLSTATE Postgres raises when an INSERT collides with
// the primary key — here, a concurrent create of the same key.
const pgUniqueViolation = "23505"

// Repo is a versioned bytea store over the kv_store table (which also holds secrets,
// in their own namespaces; the service layer handles their encryption).
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Get returns the stored value and version for a key. ok is false when absent.
func (r *Repo) Get(ctx context.Context, deploymentID, namespace, key string) (value []byte, version int64, ok bool, err error) {
	row := r.pool.QueryRow(ctx,
		`SELECT value, version FROM kv_store WHERE deployment_id = $1 AND namespace = $2 AND key = $3`,
		deploymentID, namespace, key,
	)
	if err := row.Scan(&value, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, false, nil
		}
		return nil, 0, false, fmt.Errorf("kv repo: get: %w", err)
	}
	return value, version, true, nil
}

// Write stores value using optimistic concurrency: it reads the current version
// under a row lock, compares it to expectedVersion (0 to create), and on a match
// writes version+1, returning the new version. A mismatch — or a concurrent create
// that wins the race — returns ErrVersionConflict.
func (r *Repo) Write(
	ctx context.Context, deploymentID, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("kv repo: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current int64
	exists := true
	err = tx.QueryRow(ctx,
		`SELECT version FROM kv_store WHERE deployment_id = $1 AND namespace = $2 AND key = $3 FOR UPDATE`,
		deploymentID, namespace, key,
	).Scan(&current)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		exists, current = false, 0
	case err != nil:
		return 0, fmt.Errorf("kv repo: lock: %w", err)
	}

	if expectedVersion != current {
		return 0, ErrVersionConflict
	}
	next := current + 1

	if exists {
		_, err = tx.Exec(ctx,
			`UPDATE kv_store SET value = $4, version = $5, updated_at = now()
			 WHERE deployment_id = $1 AND namespace = $2 AND key = $3`,
			deploymentID, namespace, key, value, next,
		)
	} else {
		_, err = tx.Exec(ctx,
			`INSERT INTO kv_store (deployment_id, namespace, key, value, version)
			 VALUES ($1, $2, $3, $4, $5)`,
			deploymentID, namespace, key, value, next,
		)
	}
	if err != nil {
		// A concurrent create of the same key (both saw no row) collides on the PK;
		// the loser's expectedVersion 0 is now stale, so report a conflict.
		if isUniqueViolation(err) {
			return 0, ErrVersionConflict
		}
		return 0, fmt.Errorf("kv repo: write: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("kv repo: commit: %w", err)
	}
	return next, nil
}

// Delete removes a key. expectedVersion 0 deletes unconditionally; a positive value
// must match the stored version or Delete returns ErrVersionConflict. Deleting an
// absent key is a no-op.
func (r *Repo) Delete(ctx context.Context, deploymentID, namespace, key string, expectedVersion int64) error {
	if expectedVersion == 0 {
		_, err := r.pool.Exec(ctx,
			`DELETE FROM kv_store WHERE deployment_id = $1 AND namespace = $2 AND key = $3`,
			deploymentID, namespace, key,
		)
		if err != nil {
			return fmt.Errorf("kv repo: delete: %w", err)
		}
		return nil
	}

	tag, err := r.pool.Exec(ctx,
		`DELETE FROM kv_store WHERE deployment_id = $1 AND namespace = $2 AND key = $3 AND version = $4`,
		deploymentID, namespace, key, expectedVersion,
	)
	if err != nil {
		return fmt.Errorf("kv repo: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Nothing matched: either the key is gone (fine) or the version differs.
		if _, _, ok, getErr := r.Get(ctx, deploymentID, namespace, key); getErr != nil {
			return getErr
		} else if ok {
			return ErrVersionConflict
		}
	}
	return nil
}

// DeleteByDeployment removes every key for a deployment, for best-effort cleanup
// when a deployment is undeployed.
func (r *Repo) DeleteByDeployment(ctx context.Context, deploymentID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM kv_store WHERE deployment_id = $1`, deploymentID)
	if err != nil {
		return fmt.Errorf("kv repo: delete by deployment: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a Postgres unique-violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation
}
