// Package retention holds this installation's data-retention policy — how many
// days of log events and of traces it keeps — and the sweep that enforces it.
//
// The policy is a single jsonb row in site_settings under the key "retention",
// the same table and the same convention the orchestrator's email and LLM
// settings use: a new setting costs a key rather than a migration.
//
// This service reads and writes that row itself rather than asking the
// orchestrator for it. It owns the three tables the policy governs, and the
// platform talks to it directly for exactly this data for the same reason. The
// orchestrator's sitesettings package is out of reach regardless — logs is a
// separate Go module, so its internal packages cannot be imported here — which
// is why the row access below is a small copy rather than a shared dependency.
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
// There is deliberately no locked read-modify-write here, which is the one place
// this diverges from the orchestrator's Store. That lock exists there because a
// settings save carries an already-stored API key forward, so a lost update is a
// silently discarded key rotation. A retention policy holds no secret and a save
// supplies every field, so there is nothing to carry forward: two concurrent
// saves resolve to whichever landed second either way, and a lock would only
// make that outcome slower.
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
