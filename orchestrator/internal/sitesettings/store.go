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
	// site_settings.value is jsonb; scan it straight into raw JSON so the bytes the
	// caller unmarshals are the bytes that were stored.
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
