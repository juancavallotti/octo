package agentmemory

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

// The vector half of the store: what has not been embedded yet, how a vector is
// written, and how a search ranks by one.
//
// It is separate from repo.go because it is the optional half. Everything in
// repo.go works on a deployment that has never configured an embedding provider;
// nothing here runs at all until one is.

// Pending is one row waiting for a vector.
type Pending struct {
	Kind string // HitTurn or HitUser
	ID   string
	Text string
}

// EmbeddingCounts reports how much of the store has a vector and how much does
// not, which is what the admin page shows while a backfill drains.
func (r *Repo) EmbeddingCounts(ctx context.Context) (embedded, pending int, err error) {
	row := r.pool.QueryRow(ctx,
		`SELECT
		     (SELECT count(*) FROM agent_turns WHERE embedding IS NOT NULL)
		   + (SELECT count(*) FROM agent_user_memories WHERE embedding IS NOT NULL),
		     (SELECT count(*) FROM agent_turns WHERE embedding IS NULL)
		   + (SELECT count(*) FROM agent_user_memories WHERE embedding IS NULL)`)
	if err := row.Scan(&embedded, &pending); err != nil {
		return 0, 0, fmt.Errorf("agent memory: embedding counts: %w", err)
	}
	return embedded, pending, nil
}

// PendingEmbeddings returns up to limit rows that have no vector yet, newest
// first.
//
// Newest first, deliberately. A deployment turning this on has a history that may
// be large and a present that is small, and the rows anyone is about to search
// are the recent ones — so a sweep that starts at the beginning of time is one
// where search stays useless for as long as the backlog takes. Both partial
// indexes exist to make this query cheap regardless of how much is already done.
func (r *Repo) PendingEmbeddings(ctx context.Context, limit int) ([]Pending, error) {
	// The outer LIMIT is the one that matters. A LIMIT inside each arm of a UNION
	// bounds that arm alone, so with both tables pending — the ordinary state the
	// moment an operator turns embeddings on — this returned twice the batch and
	// the sweep spent at twice the rate it documents.
	rows, err := r.pool.Query(ctx,
		`SELECT kind, id, text FROM (
		     (SELECT 'turn'::varchar AS kind, id::text AS id, text
		        FROM agent_turns
		       WHERE embedding IS NULL AND text <> ''
		       ORDER BY created_at DESC
		       LIMIT $1)
		     UNION ALL
		     (SELECT 'user'::varchar AS kind, id::text AS id, value AS text
		        FROM agent_user_memories
		       WHERE embedding IS NULL AND value <> ''
		       ORDER BY updated_at DESC
		       LIMIT $1)
		 ) pending
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("agent memory: pending embeddings: %w", err)
	}
	defer rows.Close()

	var out []Pending
	for rows.Next() {
		var p Pending
		if err := rows.Scan(&p.Kind, &p.ID, &p.Text); err != nil {
			return nil, fmt.Errorf("agent memory: scan pending: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveEmbeddings writes the vectors for a batch of rows.
//
// Each write is conditional on the row still having no vector, so a second sweep
// running concurrently cannot overwrite the first's work, and a row edited
// between the read and the write — which clears its vector — is left for the next
// pass rather than given a vector for text it no longer has.
func (r *Repo) SaveEmbeddings(ctx context.Context, rows []Pending, vectors [][]float32) error {
	if len(rows) != len(vectors) {
		return fmt.Errorf("agent memory: %d rows but %d vectors", len(rows), len(vectors))
	}
	batch := &pgx.Batch{}
	for i, row := range rows {
		literal := vectorLiteral(vectors[i])
		if row.Kind == HitUser {
			batch.Queue(
				`UPDATE agent_user_memories SET embedding = $2::vector
				  WHERE id = $1 AND embedding IS NULL`, row.ID, literal)
			continue
		}
		batch.Queue(
			`UPDATE agent_turns SET embedding = $2::vector
			  WHERE id = $1 AND embedding IS NULL`, row.ID, literal)
	}
	results := r.pool.SendBatch(ctx, batch)
	defer func() { _ = results.Close() }()
	for range rows {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("agent memory: save embedding: %w", err)
		}
	}
	return nil
}

// vectorLiteral renders a vector in pgvector's text form.
//
// Text rather than the binary protocol because that would mean registering
// pgvector's type with the pool, and a dependency on its Go package for one
// column. 'f' with -1 precision round-trips a float32 exactly, so nothing is lost
// in the encoding.
func vectorLiteral(v []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, f := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(f), 'f', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}

// SearchVector ranks by cosine distance against the HNSW indexes.
//
// The score is 1 - distance, so it reads the same way SearchText's ts_rank does:
// larger is more relevant. Callers do not branch on which ranking ran — that is
// the point of Capabilities being a fact about the store rather than two methods
// — so the two have to agree on what a score means.
//
// Rows with no vector are invisible here. That is why the caller falls back to
// text when the query returns nothing: mid-backfill, the answer may simply not be
// embedded yet.
func (r *Repo) SearchVector(
	ctx context.Context, integrationID string, q Query, vector []float32,
) ([]Hit, error) {
	limit := q.Limit
	if limit <= 0 || limit > maxSearchLimit {
		limit = defaultSearchLimit
	}
	literal := vectorLiteral(vector)
	rows, err := r.pool.Query(ctx,
		`SELECT kind, thread_key, name, text, seq, score FROM (
		     SELECT 'turn'::varchar AS kind, t.thread_key, ''::varchar AS name,
		            tn.text, tn.seq,
		            1 - (tn.embedding <=> $4::vector) AS score
		       FROM agent_turns tn
		       JOIN agent_threads t ON t.id = tn.thread_id
		      WHERE $5 <> 'user'
		        AND tn.embedding IS NOT NULL
		        AND t.integration_id = $1 AND t.agent_id = $2
		        AND ($3 = '' OR t.user_id = $3)
		        AND ($6 = '' OR t.thread_key = $6)
		      ORDER BY tn.embedding <=> $4::vector
		      LIMIT $7
		 ) turns
		 UNION ALL
		 SELECT kind, thread_key, name, text, seq, score FROM (
		     SELECT 'user'::varchar AS kind, ''::varchar AS thread_key, m.name,
		            m.value AS text, 0::bigint AS seq,
		            1 - (m.embedding <=> $4::vector) AS score
		       FROM agent_user_memories m
		      WHERE $5 <> 'turns' AND $3 <> ''
		        AND m.embedding IS NOT NULL
		        AND m.integration_id = $1 AND m.agent_id = $2 AND m.user_id = $3
		      ORDER BY m.embedding <=> $4::vector
		      LIMIT $7
		 ) memories
		 ORDER BY score DESC
		 LIMIT $7`,
		integrationID, q.AgentID, q.UserID, literal, q.Scope, q.ThreadKey, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("agent memory: vector search: %w", err)
	}
	defer rows.Close()

	var out []Hit
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Kind, &h.ThreadKey, &h.Name, &h.Text, &h.Seq, &h.Score); err != nil {
			return nil, fmt.Errorf("agent memory: scan hit: %w", err)
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ClearEmbeddings discards every stored vector.
//
// It is what a change of embedding space costs. Vectors carry no record of which
// model produced them — deliberately, because a store holding two models' vectors
// is not searchable either way and ranking only the matching subset would silently
// halve the results rather than fail — so the only way to keep the store coherent
// across a model change is to have exactly one space in it at a time.
//
// The rows are not deleted, only their vectors: the text is still there, still
// searchable by keyword, and the sweep rebuilds the vectors from it.
func (r *Repo) ClearEmbeddings(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agent memory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`UPDATE agent_turns SET embedding = NULL WHERE embedding IS NOT NULL`); err != nil {
		return fmt.Errorf("agent memory: clear turn embeddings: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE agent_user_memories SET embedding = NULL WHERE embedding IS NOT NULL`); err != nil {
		return fmt.Errorf("agent memory: clear memory embeddings: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agent memory: commit: %w", err)
	}
	return nil
}
