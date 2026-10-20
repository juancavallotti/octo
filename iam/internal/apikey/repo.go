package apikey

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// apiKeyColumns is the canonical column list (and order) scanAPIKey expects, kept
// in one place so reads and RETURNING clauses stay in sync. key_hash is never
// selected — it leaves the database only via the GetByHash lookup predicate.
const apiKeyColumns = "id, user_id, name, prefix, last4, created_at, expires_at, last_used_at, revoked_at"

const (
	// pgForeignKeyViolation is raised when user_id references no user.
	pgForeignKeyViolation = "23503"
	// pgInvalidTextRepresentation is raised when a non-UUID is passed where a uuid
	// is expected (e.g. a malformed user id in the path).
	pgInvalidTextRepresentation = "22P02"
)

// Repo persists API keys to Postgres.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// Insert stores a new key for k.UserID under the given hash, returning the
// persisted row. An unknown (or malformed) user id surfaces as ErrUserNotFound.
func (r *Repo) Insert(ctx context.Context, k APIKey, keyHash string) (APIKey, error) {
	row := r.pool.QueryRow(ctx,
		`INSERT INTO api_keys (user_id, name, key_hash, prefix, last4, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING `+apiKeyColumns,
		k.UserID, k.Name, keyHash, k.Prefix, k.Last4, k.ExpiresAt,
	)
	out, err := scanAPIKey(row)
	if err != nil {
		switch pgErrorCode(err) {
		case pgForeignKeyViolation, pgInvalidTextRepresentation:
			return APIKey{}, ErrUserNotFound
		}
		return APIKey{}, fmt.Errorf("apikey repo: insert: %w", err)
	}
	return out, nil
}

// ListByUser returns a user's active (non-revoked) keys, newest first.
func (r *Repo) ListByUser(ctx context.Context, userID string) ([]APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+apiKeyColumns+`
		 FROM api_keys
		 WHERE user_id = $1 AND revoked_at IS NULL
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("apikey repo: list by user: %w", err)
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (APIKey, error) {
		return scanAPIKey(row)
	})
	if err != nil {
		// A malformed user id surfaces during the row scan, not the query.
		if pgErrorCode(err) == pgInvalidTextRepresentation {
			return []APIKey{}, nil
		}
		return nil, fmt.Errorf("apikey repo: list by user: %w", err)
	}
	return items, nil
}

// GetByHash returns the active key whose token hashes to keyHash, or ErrNotFound.
func (r *Repo) GetByHash(ctx context.Context, keyHash string) (APIKey, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT `+apiKeyColumns+`
		 FROM api_keys
		 WHERE key_hash = $1 AND revoked_at IS NULL`,
		keyHash,
	)
	out, err := scanAPIKey(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return APIKey{}, ErrNotFound
		}
		return APIKey{}, fmt.Errorf("apikey repo: get by hash: %w", err)
	}
	return out, nil
}

// Revoke soft-deletes the key, scoped to its owner so one user cannot revoke
// another's. Returns ErrNotFound when no active key matches both id and userID.
func (r *Repo) Revoke(ctx context.Context, id, userID string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked_at = now()
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`,
		id, userID,
	)
	if err != nil {
		if pgErrorCode(err) == pgInvalidTextRepresentation {
			return ErrNotFound
		}
		return fmt.Errorf("apikey repo: revoke: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchLastUsed records that the key was just used. Best-effort: callers ignore
// the error since a failed bump must not fail the authenticated request.
func (r *Repo) TouchLastUsed(ctx context.Context, id string) error {
	_, err := r.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = now() WHERE id = $1`, id)
	return err
}

// scanAPIKey reads one row in apiKeyColumns order.
func scanAPIKey(row pgx.Row) (APIKey, error) {
	var k APIKey
	if err := row.Scan(
		&k.ID, &k.UserID, &k.Name, &k.Prefix, &k.Last4,
		&k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt,
	); err != nil {
		return APIKey{}, err
	}
	return k, nil
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
