package agentmemory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repo is the database half: agent_threads, agent_working_memory, agent_turns
// and agent_user_memories.
//
// Every write that can race carries the version the caller read, and a stale one
// is refused rather than silently winning. The exception is AppendTurns, and
// deliberately: appends to a conversation commute, so demanding a version would
// make two replicas of one agent fight over a log that has no conflict to
// detect. Their order comes from the row lock the thread update takes.
type Repo struct {
	pool *pgxpool.Pool
}

// NewRepo returns a Repo backed by the given pool.
func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

// threadColumns is the select list every thread read shares, in scan order.
const threadColumns = `id, agent_id, thread_key, user_id, title, version, turn_count,
	created_at, last_activity_at`

// scanThread reads one row of threadColumns.
func scanThread(row pgx.Row) (Thread, error) {
	var t Thread
	err := row.Scan(&t.ID, &t.AgentID, &t.ThreadKey, &t.UserID, &t.Title,
		&t.Version, &t.TurnCount, &t.CreatedAt, &t.LastActivityAt)
	return t, err
}

// ensureThread returns the id of a conversation, creating it if this is the
// first write to it.
//
// There is no CreateThread on the surface: a conversation comes into existence
// because something was written to it, which is the only moment anyone knows it
// exists. The insert is ON CONFLICT DO NOTHING followed by a select, so two
// concurrent first-writes settle on one row rather than one of them failing.
func ensureThread(ctx context.Context, tx pgx.Tx, ref Ref) (string, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO agent_threads (integration_id, agent_id, thread_key, user_id)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (integration_id, agent_id, thread_key) DO NOTHING`,
		ref.IntegrationID, ref.AgentID, ref.ThreadKey, ref.UserID,
	); err != nil {
		return "", fmt.Errorf("agent memory: create thread: %w", err)
	}
	var id string
	err := tx.QueryRow(ctx,
		`SELECT id FROM agent_threads
		  WHERE integration_id = $1 AND agent_id = $2 AND thread_key = $3`,
		ref.IntegrationID, ref.AgentID, ref.ThreadKey,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("agent memory: find thread: %w", err)
	}
	// A conversation opened before anyone knew who it was with adopts the user on
	// the first write that names one. Never the other way round: a thread that
	// already belongs to somebody is not reassigned by a later write that forgot.
	if ref.UserID != "" {
		if _, err := tx.Exec(ctx,
			`UPDATE agent_threads SET user_id = $2 WHERE id = $1 AND user_id = ''`,
			id, ref.UserID,
		); err != nil {
			return "", fmt.Errorf("agent memory: attribute thread: %w", err)
		}
	}
	return id, nil
}

// LoadWorking returns a conversation's live context. ok is false when there is
// none yet.
func (r *Repo) LoadWorking(ctx context.Context, ref Ref) (Working, bool, error) {
	var w Working
	err := r.pool.QueryRow(ctx,
		`SELECT w.version, w.iteration, w.tokens, w.payload, w.updated_at
		   FROM agent_working_memory w
		   JOIN agent_threads t ON t.id = w.thread_id
		  WHERE t.integration_id = $1 AND t.agent_id = $2 AND t.thread_key = $3`,
		ref.IntegrationID, ref.AgentID, ref.ThreadKey,
	).Scan(&w.Version, &w.Iteration, &w.Tokens, &w.Payload, &w.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Working{}, false, nil
		}
		return Working{}, false, fmt.Errorf("agent memory: load working: %w", err)
	}
	return w, true, nil
}

// SaveWorking stores the live context, creating the conversation if it is new,
// and returns the new version.
//
// The WHERE on the update arm is the concurrency check: it matches no row when
// the stored version has moved on, the statement returns nothing, and the caller
// gets a conflict. The insert arm covers expectedVersion 0, and a conflict with 0
// correctly returns nothing because 0 never equals a stored version.
func (r *Repo) SaveWorking(ctx context.Context, ref Ref, w Working) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("agent memory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	threadID, err := ensureThread(ctx, tx, ref)
	if err != nil {
		return 0, err
	}
	// Two arms, and they have to be separate statements rather than an upsert.
	//
	// An upsert cannot express "create only when the caller expected to create":
	// its insert always fires, so a write stating version 5 against working memory
	// that has since been erased would land as a fresh row at version 1 and report
	// success. That is the lost update optimistic concurrency exists to catch, and
	// erasure makes it reachable rather than hypothetical.
	//
	// So: update when the version still matches, insert only when the caller said
	// 0, and neither matching is the conflict. A stored version is never 0 — a row
	// is only ever created at 1 — so the two arms cannot both fire.
	var version int64
	err = tx.QueryRow(ctx,
		`WITH updated AS (
		     UPDATE agent_working_memory
		        SET version    = version + 1,
		            iteration  = $3,
		            tokens     = $4,
		            payload    = $5,
		            updated_at = now()
		      WHERE thread_id = $1 AND version = $2
		     RETURNING version
		 ), inserted AS (
		     INSERT INTO agent_working_memory (thread_id, version, iteration, tokens, payload)
		     SELECT $1, 1, $3, $4, $5 WHERE $2::bigint = 0
		     ON CONFLICT (thread_id) DO NOTHING
		     RETURNING version
		 )
		 SELECT version FROM updated
		 UNION ALL
		 SELECT version FROM inserted`,
		threadID, w.Version, w.Iteration, w.Tokens, w.Payload,
	).Scan(&version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrVersionConflict
		}
		return 0, fmt.Errorf("agent memory: save working: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE agent_threads SET last_activity_at = now(), version = version + 1 WHERE id = $1`,
		threadID,
	); err != nil {
		return 0, fmt.Errorf("agent memory: touch thread: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("agent memory: commit: %w", err)
	}
	return version, nil
}

// AppendTurns adds completed turns to the durable record and returns the
// conversation's new version.
//
// The sequence numbers come from turn_count under the row lock the UPDATE takes,
// so two writers on one conversation interleave rather than collide. That is why
// this needs no expected version from the caller.
func (r *Repo) AppendTurns(ctx context.Context, ref Ref, turns []Turn) (int64, error) {
	if len(turns) == 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("agent memory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	threadID, err := ensureThread(ctx, tx, ref)
	if err != nil {
		return 0, err
	}
	var base int64
	var version int64
	err = tx.QueryRow(ctx,
		`UPDATE agent_threads
		    SET turn_count       = turn_count + $2,
		        version          = version + 1,
		        last_activity_at = now()
		  WHERE id = $1
		 RETURNING turn_count - $2, version`,
		threadID, len(turns),
	).Scan(&base, &version)
	if err != nil {
		return 0, fmt.Errorf("agent memory: reserve sequence: %w", err)
	}

	rows := make([][]any, 0, len(turns))
	for i, turn := range turns {
		attrs, marshalErr := json.Marshal(turnAttrs(turn.Attrs))
		if marshalErr != nil {
			return 0, fmt.Errorf("agent memory: encode turn attrs: %w", marshalErr)
		}
		rows = append(rows, []any{threadID, base + int64(i) + 1, turn.Role, turn.Text, turn.Tokens, attrs})
	}
	if _, err := tx.CopyFrom(ctx,
		pgx.Identifier{"agent_turns"},
		[]string{"thread_id", "seq", "role", "text", "tokens", "attrs"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return 0, fmt.Errorf("agent memory: append turns: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("agent memory: commit: %w", err)
	}
	return version, nil
}

// turnAttrs normalizes a turn's opaque attributes so the column is never NULL.
func turnAttrs(attrs map[string]any) map[string]any {
	if attrs == nil {
		return map[string]any{}
	}
	return attrs
}

// ListThreads returns an agent's conversations, most recently active first.
//
// The cursor is a keyset on (last_activity_at, id), matching the listing index,
// so it neither skips nor repeats a row when a conversation is written to
// between two pages — which an offset would do constantly, since writing to a
// conversation is exactly what reorders the listing.
func (r *Repo) ListThreads(
	ctx context.Context, integrationID, agentID, userID string, page Page,
) ([]Thread, string, error) {
	limit := page.Limit
	if limit <= 0 || limit > maxPageLimit {
		limit = defaultPageLimit
	}
	after, afterID, err := decodeThreadCursor(page.Cursor)
	if err != nil {
		return nil, "", err
	}
	// nil rather than "" for the id half of the cursor. Postgres casts a bound
	// parameter before it evaluates the OR, so an empty string reaching $5::uuid is
	// a syntax error on every unpaged listing — the short-circuit on $4 never gets
	// a chance to save it.
	var afterUUID any
	if afterID != "" {
		afterUUID = afterID
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+threadColumns+`
		   FROM agent_threads
		  WHERE integration_id = $1
		    AND agent_id = $2
		    AND ($3 = '' OR user_id = $3)
		    AND ($4::timestamptz IS NULL
		         OR (last_activity_at, id) < ($4::timestamptz, $5::uuid))
		  ORDER BY last_activity_at DESC, id DESC
		  LIMIT $6`,
		integrationID, agentID, userID, after, afterUUID, limit+1,
	)
	if err != nil {
		return nil, "", fmt.Errorf("agent memory: list threads: %w", err)
	}
	defer rows.Close()

	out := make([]Thread, 0, limit)
	for rows.Next() {
		t, scanErr := scanThread(rows)
		if scanErr != nil {
			return nil, "", fmt.Errorf("agent memory: scan thread: %w", scanErr)
		}
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("agent memory: list threads: %w", err)
	}
	// One more than asked for is how the listing knows there is another page
	// without a second count query.
	next := ""
	if len(out) > limit {
		out = out[:limit]
		last := out[len(out)-1]
		next = encodeThreadCursor(last.LastActivityAt, last.ID)
	}
	return out, next, nil
}

// ReadThread returns a conversation and a page of its turns, oldest first.
func (r *Repo) ReadThread(ctx context.Context, ref Ref, page Page) (Thread, []Turn, string, error) {
	thread, err := scanThread(r.pool.QueryRow(ctx,
		`SELECT `+threadColumns+`
		   FROM agent_threads
		  WHERE integration_id = $1 AND agent_id = $2 AND thread_key = $3`,
		ref.IntegrationID, ref.AgentID, ref.ThreadKey,
	))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Thread{}, nil, "", ErrNotFound
		}
		return Thread{}, nil, "", fmt.Errorf("agent memory: read thread: %w", err)
	}
	limit := page.Limit
	if limit <= 0 || limit > maxTurnLimit {
		limit = defaultTurnLimit
	}
	afterSeq, err := decodeSeqCursor(page.Cursor)
	if err != nil {
		return Thread{}, nil, "", err
	}
	rows, err := r.pool.Query(ctx,
		`SELECT seq, role, text, tokens, attrs, created_at
		   FROM agent_turns
		  WHERE thread_id = $1 AND seq > $2
		  ORDER BY seq
		  LIMIT $3`,
		thread.ID, afterSeq, limit+1,
	)
	if err != nil {
		return Thread{}, nil, "", fmt.Errorf("agent memory: read turns: %w", err)
	}
	defer rows.Close()

	turns := make([]Turn, 0, limit)
	for rows.Next() {
		var t Turn
		var attrs []byte
		if err := rows.Scan(&t.Seq, &t.Role, &t.Text, &t.Tokens, &attrs, &t.CreatedAt); err != nil {
			return Thread{}, nil, "", fmt.Errorf("agent memory: scan turn: %w", err)
		}
		if len(attrs) > 0 {
			_ = json.Unmarshal(attrs, &t.Attrs)
		}
		turns = append(turns, t)
	}
	if err := rows.Err(); err != nil {
		return Thread{}, nil, "", fmt.Errorf("agent memory: read turns: %w", err)
	}
	next := ""
	if len(turns) > limit {
		turns = turns[:limit]
		next = encodeSeqCursor(turns[len(turns)-1].Seq)
	}
	return thread, turns, next, nil
}

// DeleteThread erases a conversation. The cascade takes its working memory and
// its turns with it, which is the point: a delete that left the transcript
// behind would report success over a readable copy.
func (r *Repo) DeleteThread(ctx context.Context, ref Ref) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM agent_threads
		  WHERE integration_id = $1 AND agent_id = $2 AND thread_key = $3`,
		ref.IntegrationID, ref.AgentID, ref.ThreadKey,
	)
	if err != nil {
		return fmt.Errorf("agent memory: delete thread: %w", err)
	}
	return nil
}

// SetTitle names a conversation, creating it if the caller is naming one it is
// about to write to.
func (r *Repo) SetTitle(ctx context.Context, ref Ref, title string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agent memory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	threadID, err := ensureThread(ctx, tx, ref)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE agent_threads SET title = $2, version = version + 1 WHERE id = $1`,
		threadID, title,
	); err != nil {
		return fmt.Errorf("agent memory: set title: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agent memory: commit: %w", err)
	}
	return nil
}

// ListAgents summarizes which agents have memory under an integration, so an
// operator has something to pick from without knowing the ids in advance.
func (r *Repo) ListAgents(ctx context.Context, integrationID string) ([]AgentSummary, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT agent_id, count(*), max(last_activity_at)
		   FROM agent_threads
		  WHERE integration_id = $1
		  GROUP BY agent_id
		  ORDER BY max(last_activity_at) DESC`,
		integrationID,
	)
	if err != nil {
		return nil, fmt.Errorf("agent memory: list agents: %w", err)
	}
	defer rows.Close()

	var out []AgentSummary
	for rows.Next() {
		var a AgentSummary
		if err := rows.Scan(&a.AgentID, &a.ThreadCount, &a.LastActivityAt); err != nil {
			return nil, fmt.Errorf("agent memory: scan agent: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Memories returns everything an agent has kept about one person.
func (r *Repo) Memories(ctx context.Context, ref Ref) ([]UserMemory, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT name, value, version, created_at, updated_at
		   FROM agent_user_memories
		  WHERE integration_id = $1 AND agent_id = $2 AND user_id = $3
		  ORDER BY name`,
		ref.IntegrationID, ref.AgentID, ref.UserID,
	)
	if err != nil {
		return nil, fmt.Errorf("agent memory: list memories: %w", err)
	}
	defer rows.Close()

	var out []UserMemory
	for rows.Next() {
		var m UserMemory
		if err := rows.Scan(&m.Name, &m.Value, &m.Version, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("agent memory: scan memory: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PutMemory creates or updates one curated memory, with the same
// optimistic-concurrency shape as SaveWorking.
func (r *Repo) PutMemory(ctx context.Context, ref Ref, name, value string, expectedVersion int64) (int64, error) {
	// The same two arms as SaveWorking, and for the same reason: a correction
	// stating a version that was forgotten in between must be told it is stale, not
	// quietly re-created. embedding is cleared on every change — a vector for text
	// that no longer exists is worse than no vector, because search would rank on
	// it.
	var version int64
	err := r.pool.QueryRow(ctx,
		`WITH updated AS (
		     UPDATE agent_user_memories
		        SET value      = $5,
		            version    = version + 1,
		            updated_at = now(),
		            embedding  = NULL
		      WHERE integration_id = $1 AND agent_id = $2 AND user_id = $3
		        AND name = $4 AND version = $6
		     RETURNING version
		 ), inserted AS (
		     INSERT INTO agent_user_memories (integration_id, agent_id, user_id, name, value, version)
		     SELECT $1, $2, $3, $4, $5, 1 WHERE $6::bigint = 0
		     ON CONFLICT (integration_id, agent_id, user_id, name) DO NOTHING
		     RETURNING version
		 )
		 SELECT version FROM updated
		 UNION ALL
		 SELECT version FROM inserted`,
		ref.IntegrationID, ref.AgentID, ref.UserID, name, value, expectedVersion,
	).Scan(&version)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, ErrVersionConflict
		}
		return 0, fmt.Errorf("agent memory: put memory: %w", err)
	}
	return version, nil
}

// DeleteMemory forgets one curated memory. A name that was never stored is not
// an error: the end state the caller asked for is the end state they have.
func (r *Repo) DeleteMemory(ctx context.Context, ref Ref, name string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM agent_user_memories
		  WHERE integration_id = $1 AND agent_id = $2 AND user_id = $3 AND name = $4`,
		ref.IntegrationID, ref.AgentID, ref.UserID, name,
	)
	if err != nil {
		return fmt.Errorf("agent memory: delete memory: %w", err)
	}
	return nil
}

// DeleteForIntegration removes everything an integration's agents remember. It
// is what integration deletion calls: there is no foreign key doing it, by the
// same choice logs and traces make, so the cleanup is explicit and visible.
func (r *Repo) DeleteForIntegration(ctx context.Context, integrationID string) error {
	// One transaction, because a half-done sweep is worse than a failed one: the
	// caller logs and does not retry, so conversations gone with the curated
	// memories left behind would leave rows with no owner and no way to reach them
	// through an API that addresses everything by integration.
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("agent memory: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`DELETE FROM agent_threads WHERE integration_id = $1`, integrationID); err != nil {
		return fmt.Errorf("agent memory: delete threads: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM agent_user_memories WHERE integration_id = $1`, integrationID); err != nil {
		return fmt.Errorf("agent memory: delete memories: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("agent memory: commit: %w", err)
	}
	return nil
}

// SearchText is the fallback every deployment gets: Postgres full-text over
// turns and curated memories, ranked by ts_rank.
//
// It is what runs when no embedding provider is configured, and it has to be
// good on its own — a semantic index is an improvement on this, not a
// precondition for search working at all.
func (r *Repo) SearchText(ctx context.Context, integrationID string, q Query) ([]Hit, error) {
	limit := q.Limit
	if limit <= 0 || limit > maxSearchLimit {
		limit = defaultSearchLimit
	}
	// The terms are OR'd, not AND'd, and ts_rank does the rest.
	//
	// websearch_to_tsquery ANDs bare words, which is right for a search box and
	// wrong for this: an agent asking "deployment rollout problems" wants the turns
	// about deployments and rollouts, ranked, not nothing because no single turn
	// contains all three. Verified on a live store — that exact query matched two
	// obviously relevant turns and returned neither.
	//
	// Rewriting the operator rather than switching parser keeps what websearch is
	// good at: a quoted "roll out" stays a phrase, and a leading - stays a negation.
	// It only relaxes the joins between terms, which is the part that was a cliff.
	rows, err := r.pool.Query(ctx,
		`WITH q AS (
		     SELECT replace(websearch_to_tsquery('simple', $4)::text, '&', '|')::tsquery AS tsq
		 )
		 SELECT kind, thread_key, name, text, seq, score FROM (
		     SELECT 'turn'::varchar AS kind, t.thread_key, ''::varchar AS name,
		            tn.text, tn.seq,
		            ts_rank(to_tsvector('simple', tn.text), q.tsq) AS score
		       FROM agent_turns tn
		       JOIN agent_threads t ON t.id = tn.thread_id, q
		      WHERE $5 <> 'user'
		        AND t.integration_id = $1 AND t.agent_id = $2
		        AND ($3 = '' OR t.user_id = $3)
		        AND ($6 = '' OR t.thread_key = $6)
		        AND to_tsvector('simple', tn.text) @@ q.tsq
		     UNION ALL
		     SELECT 'user'::varchar, ''::varchar, m.name, m.value, 0::bigint,
		            ts_rank(to_tsvector('simple', m.value), q.tsq)
		       FROM agent_user_memories m, q
		      WHERE $5 <> 'turns'
		        AND m.integration_id = $1 AND m.agent_id = $2 AND m.user_id = $3
		        AND $3 <> ''
		        AND to_tsvector('simple', m.value) @@ q.tsq
		 ) hits
		 ORDER BY score DESC
		 LIMIT $7`,
		integrationID, q.AgentID, q.UserID, q.Text, q.Scope, q.ThreadKey, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("agent memory: search: %w", err)
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

// Paging limits. The defaults are what a UI asks for; the maxima are what stops
// a caller asking for the whole store in one request.
const (
	defaultPageLimit   = 50
	maxPageLimit       = 200
	defaultTurnLimit   = 200
	maxTurnLimit       = 1000
	defaultSearchLimit = 10
	maxSearchLimit     = 100
)
