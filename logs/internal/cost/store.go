package cost

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store keeps the rate card in Postgres as history rather than as current state.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore returns a store backed by pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Sync is the outcome of one attempt to refresh a source's card, successful or
// not. Recording the failures is the point: a feed that quietly stopped answering
// looks exactly like a feed whose prices stopped changing, and only one of those
// means the costs being stored are drifting away from reality.
type Sync struct {
	Source  string
	Models  int
	Added   int
	Changed int
	Err     error
}

// Current returns the open rows of a source's card — the rates in force now.
//
// It is also how a refresh learns the ids of what it just wrote: rates come off
// a fetch with no identity, and the id is what every priced record points back
// at, so a card is built from what the database holds rather than from what was
// sent to it.
func (s *Store) Current(ctx context.Context, source string) ([]Rate, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, provider, model, operator,
		        input_per_1m, output_per_1m, cache_read_per_1m, cache_write_per_1m, preferred
		   FROM llm_prices
		  WHERE source = $1 AND effective_to IS NULL`, source)
	if err != nil {
		return nil, fmt.Errorf("cost: query prices: %w", err)
	}
	defer rows.Close()

	var out []Rate
	for rows.Next() {
		var rate Rate
		var operator string
		if err := rows.Scan(
			&rate.ID, &rate.Provider, &rate.Pattern, &operator,
			&rate.InputPer1M, &rate.OutputPer1M,
			&rate.CacheReadPer1M, &rate.CacheWritePer1M, &rate.Preferred,
		); err != nil {
			return nil, fmt.Errorf("cost: scan price: %w", err)
		}
		rate.Operator = Operator(operator)
		out = append(out, rate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("cost: iterate prices: %w", err)
	}
	return out, nil
}

// Apply closes superseded rows and opens their replacements.
//
// The whole set must land or none of it must, because the card is uniquely
// indexed on one open row per key: a partial apply could leave a key with two
// open rows or with none, and neither is a state the rest of this package knows
// how to read. A close is therefore queued immediately before the insert that
// replaces it, since the index would reject the insert while the old row is
// still open.
//
// The explicit transaction is belt-and-braces rather than the mechanism.
// Postgres already runs one pipelined batch as an implicit transaction, so today
// the BEGIN changes nothing — a mutation test removing it fails nothing. It is
// here so the guarantee survives this function growing a second batch, which
// chunking a large card would require and which would silently lose atomicity
// without it.
//
// Callers reload with Current afterwards rather than assuming what they wrote:
// the ids assigned here are what priced records point back at.
func (s *Store) Apply(ctx context.Context, source string, changes []Change) error {
	if len(changes) == 0 {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("cost: begin price update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	for _, change := range changes {
		if change.Supersedes != "" {
			batch.Queue(
				`UPDATE llm_prices SET effective_to = now()
				  WHERE id = $1::uuid AND effective_to IS NULL`, change.Supersedes)
		}
		batch.Queue(
			`INSERT INTO llm_prices
			   (source, provider, model, operator,
			    input_per_1m, output_per_1m, cache_read_per_1m, cache_write_per_1m, preferred)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			source, change.Rate.Provider, change.Rate.Pattern, string(change.Rate.Operator),
			change.Rate.InputPer1M, change.Rate.OutputPer1M,
			change.Rate.CacheReadPer1M, change.Rate.CacheWritePer1M, change.Rate.Preferred)
	}

	results := tx.SendBatch(ctx, batch)
	if err := results.Close(); err != nil {
		return fmt.Errorf("cost: apply price changes: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("cost: commit price update: %w", err)
	}
	return nil
}

// RecordSync stores the outcome of one refresh attempt.
func (s *Store) RecordSync(ctx context.Context, sync Sync) error {
	message := ""
	if sync.Err != nil {
		message = sync.Err.Error()
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO llm_price_syncs (source, models, added, changed, ok, error)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		sync.Source, sync.Models, sync.Added, sync.Changed, sync.Err == nil, message)
	if err != nil {
		return fmt.Errorf("cost: record price sync: %w", err)
	}
	return nil
}
