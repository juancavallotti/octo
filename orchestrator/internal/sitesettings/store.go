// Package sitesettings holds configuration that belongs to the installation
// rather than to any integration, deployment or user: the email provider the site
// sends through, the LLM provider it reasons with, and whatever follows them.
//
// Everything lives in the site_settings table, one jsonb row per feature keyed by
// a short string, so a new feature costs a key rather than a migration. This
// package owns two things and no domain logic: the row-level get/put, and
// SecretField, the encrypted-at-rest API key that every one of those features
// happens to need.
package sitesettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store reads and writes site_settings rows. Values are opaque JSON here; the
// feature package that owns a key is the only thing that knows its shape.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a Store backed by the given pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Get returns the raw JSON stored under key. ok is false when the row is absent,
// which is a normal state (nothing configured yet) rather than an error.
func (s *Store) Get(ctx context.Context, key string) (json.RawMessage, bool, error) {
	// site_settings.value is jsonb; scan it straight into raw JSON and let the
	// caller unmarshal. Note jsonb is a parsed representation, not the text that was
	// written — key order and whitespace are Postgres's on the way out — so this
	// round-trips the value, not the bytes.
	var value json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM site_settings WHERE key = $1`, key,
	).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("site settings get %q: %w", key, err)
	}
	return value, true, nil
}

// Put stores value under key, creating the row or replacing it.
func (s *Store) Put(ctx context.Context, key string, value json.RawMessage) error {
	// pgx infers the jsonb OID from the column and passes []byte through as
	// pre-encoded JSON, so the marshalled value needs no further wrapping.
	_, err := s.pool.Exec(ctx,
		`INSERT INTO site_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
	if err != nil {
		return fmt.Errorf("site settings put %q: %w", key, err)
	}
	return nil
}

// Mutate applies fn to the value under key and writes the result back, holding a row
// lock across the read and the write.
//
// A settings save is a read-modify-write: it reads the stored row to carry the
// existing API key forward, then writes the whole row back. Done with a plain Get
// then Put, two concurrent saves interleave and the second silently discards the
// first — and because the key is the thing being carried forward, the value most
// likely to be lost is a key rotation. That is precisely the failure
// SecretField.Apply exists to prevent, so it should not be reachable by another
// route.
//
// fn receives the stored JSON and whether the row already existed.
func (s *Store) Mutate(
	ctx context.Context,
	key string,
	fn func(current json.RawMessage, ok bool) (json.RawMessage, error),
) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("site settings mutate %q: begin: %w", key, err)
	}
	// Rolled back unless the commit below succeeds; a rollback after commit is a
	// no-op, so this needs no flag to guard it.
	defer func() { _ = tx.Rollback(ctx) }()

	// FOR UPDATE cannot lock a row that does not exist, so materialise it first. DO
	// NOTHING makes a concurrent creator harmless: whoever loses simply sees zero
	// rows affected and reads the winner's row below. RowsAffected is also how we
	// know whether the settings existed before this call, which fn needs and an
	// empty-object placeholder could not tell us.
	tag, err := tx.Exec(ctx,
		`INSERT INTO site_settings (key, value) VALUES ($1, '{}'::jsonb)
		 ON CONFLICT (key) DO NOTHING`, key)
	if err != nil {
		return fmt.Errorf("site settings mutate %q: reserve: %w", key, err)
	}
	created := tag.RowsAffected() == 1

	var current json.RawMessage
	if err := tx.QueryRow(ctx,
		`SELECT value FROM site_settings WHERE key = $1 FOR UPDATE`, key,
	).Scan(&current); err != nil {
		return fmt.Errorf("site settings mutate %q: lock: %w", key, err)
	}

	next, err := fn(current, !created)
	if err != nil {
		// The caller rejected the change (a validation failure, a missing cipher).
		// Returning it unwrapped keeps errors.Is working on their own sentinels.
		return err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE site_settings SET value = $2 WHERE key = $1`, key, next); err != nil {
		return fmt.Errorf("site settings mutate %q: write: %w", key, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("site settings mutate %q: commit: %w", key, err)
	}
	return nil
}
