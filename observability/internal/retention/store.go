// Package retention holds this installation's data-retention policy — how many
// days of log events and of traces it keeps — and the sweep that enforces it.
//
// The policy is a single jsonb row in site_settings under the key "retention", so
// a new setting costs a key rather than a migration. This service reads and writes
// that row itself, because it owns the three tables the policy governs.
package retention

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store reads and writes one site_settings row. Values are opaque JSON here; the
// shape belongs to the feature that owns the key.
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
	// caller unmarshal. Note jsonb is a parsed representation, not the text that
	// was written — key order and whitespace are Postgres's on the way out — so
	// this round-trips the value, not the bytes.
	var value json.RawMessage
	err := s.pool.QueryRow(ctx,
		`SELECT value FROM site_settings WHERE key = $1`, key,
	).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("retention: site settings get %q: %w", key, err)
	}
	return value, true, nil
}

// Put stores value under key, creating the row or replacing it.
//
// There is no locked read-modify-write here: a retention policy holds no secret
// and a save supplies every field, so there is nothing to carry forward. Two
// concurrent saves resolve to whichever landed second either way.
func (s *Store) Put(ctx context.Context, key string, value json.RawMessage) error {
	// pgx infers the jsonb OID from the column and passes []byte through as
	// pre-encoded JSON, so the marshalled value needs no further wrapping.
	_, err := s.pool.Exec(ctx,
		`INSERT INTO site_settings (key, value) VALUES ($1, $2)
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value)
	if err != nil {
		return fmt.Errorf("retention: site settings put %q: %w", key, err)
	}
	return nil
}
