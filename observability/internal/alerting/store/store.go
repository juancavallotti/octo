// Package store persists watches, their state, their episodes and every
// evaluation that was ever run.
//
// It is separate from the alerting package for the same reason source is: what
// this feature is worth is decided by pure functions over value types, and
// keeping the code that needs a server on this side of the boundary is what makes
// those testable without one. The alerting package declares the narrow interfaces
// it consumes; *Store happens to satisfy them.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// uniqueViolation is the SQLSTATE the case-insensitive name index raises. The
// service pre-checks for a clean error and this is the backstop against races and
// direct writes, so it is translated rather than surfaced as a database failure.
const uniqueViolation = "23505"

// Store reads and writes everything alerting keeps in Postgres.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a store over pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// watchColumns is the SELECT list every watch read shares, in the order scanWatch
// expects. One constant rather than four copies, because a column added to the
// table and to three of the four queries is a bug that only shows up on the path
// nobody exercised.
const watchColumns = `
    w.id, w.name, w.description, w.enabled, w.severity,
    w.combinator, w.conditions, w.actions, w.on_no_data,
    w.step_seconds, w.interval_seconds, w.for_seconds, w.renotify_seconds,
    w.definition_hash, w.created_at, w.updated_at`

func scanWatch(row pgx.Row) (alerting.Watch, error) {
	var w alerting.Watch
	var conditions, actions []byte
	var step, interval, hold, renotify int
	err := row.Scan(&w.ID, &w.Name, &w.Description, &w.Enabled, &w.Severity,
		&w.Combinator, &conditions, &actions, &w.OnNoData,
		&step, &interval, &hold, &renotify,
		&w.DefinitionHash, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return alerting.Watch{}, err
	}
	w.Step = time.Duration(step) * time.Second
	w.Interval = time.Duration(interval) * time.Second
	w.For = time.Duration(hold) * time.Second
	w.Renotify = time.Duration(renotify) * time.Second

	// A definition this process cannot decode is an error, never a partial
	// watch. Evaluating a set with a condition silently missing changes what
	// `all` and `any` mean, in opposite directions, and neither would be noticed.
	if err := json.Unmarshal(conditions, &w.Conditions); err != nil {
		return alerting.Watch{}, fmt.Errorf("store: decode the conditions of watch %s: %w", w.ID, err)
	}
	if err := json.Unmarshal(actions, &w.Actions); err != nil {
		return alerting.Watch{}, fmt.Errorf("store: decode the actions of watch %s: %w", w.ID, err)
	}
	return w, nil
}

// Create inserts a watch and its state row together, so a watch is never visible
// without somewhere for its first evaluation to land.
//
// The state's next due time is now: a watch somebody just saved should be
// answered on the next tick rather than after one interval of silence.
func (s *Store) Create(ctx context.Context, w alerting.Watch, createdBy string) (alerting.Watch, error) {
	hash, err := w.Fingerprint()
	if err != nil {
		return alerting.Watch{}, err
	}
	conditions, actions, err := encodeDefinition(w)
	if err != nil {
		return alerting.Watch{}, err
	}

	var out alerting.Watch
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO alert_watches (name, description, enabled, severity, combinator,
			                           conditions, actions, on_no_data, step_seconds,
			                           interval_seconds, for_seconds, renotify_seconds,
			                           definition_hash, created_by, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,`+nullableUUID(14)+`,`+nullableUUID(14)+`)
			RETURNING `+strip(watchColumns),
			w.Name, w.Description, w.Enabled, w.Severity, w.Combinator,
			conditions, actions, w.OnNoData, seconds(w.Step),
			seconds(w.Interval), seconds(w.For), seconds(w.Renotify), hash, nullOrString(createdBy))
		created, err := scanWatch(row)
		if err != nil {
			return err
		}
		out = created
		_, err = tx.Exec(ctx, `
			INSERT INTO alert_watch_state (watch_id, definition_hash, next_due_at)
			VALUES ($1::uuid, $2, now())`, created.ID, hash)
		return err
	})
	if err != nil {
		return alerting.Watch{}, translate(err, "create a watch")
	}
	return out, nil
}

// Update replaces a definition in place.
//
// The state row is not reset here. Step does that on the next evaluation, when it
// sees the stored hash no longer matches — which keeps every reason a hold can
// restart in the one function that owns holds, rather than splitting it between
// the editor and the evaluator.
func (s *Store) Update(ctx context.Context, w alerting.Watch, updatedBy string) (alerting.Watch, error) {
	hash, err := w.Fingerprint()
	if err != nil {
		return alerting.Watch{}, err
	}
	conditions, actions, err := encodeDefinition(w)
	if err != nil {
		return alerting.Watch{}, err
	}
	row := s.pool.QueryRow(ctx, `
		UPDATE alert_watches AS w
		   SET name = $2, description = $3, enabled = $4, severity = $5, combinator = $6,
		       conditions = $7, actions = $8, on_no_data = $9, step_seconds = $10,
		       interval_seconds = $11, for_seconds = $12, renotify_seconds = $13,
		       definition_hash = $14, updated_at = now(), updated_by = `+nullableUUID(15)+`
		 WHERE w.id = $1::uuid
		RETURNING `+watchColumns,
		w.ID, w.Name, w.Description, w.Enabled, w.Severity, w.Combinator,
		conditions, actions, w.OnNoData, seconds(w.Step),
		seconds(w.Interval), seconds(w.For), seconds(w.Renotify), hash, nullOrString(updatedBy))

	out, err := scanWatch(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return alerting.Watch{}, alerting.ErrWatchNotFound
	}
	if err != nil {
		return alerting.Watch{}, translate(err, "update a watch")
	}
	return out, nil
}

// Get reads one watch.
func (s *Store) Get(ctx context.Context, id string) (alerting.Watch, error) {
	w, err := scanWatch(s.pool.QueryRow(ctx,
		`SELECT `+watchColumns+` FROM alert_watches AS w WHERE w.id = $1::uuid`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return alerting.Watch{}, alerting.ErrWatchNotFound
	}
	if err != nil {
		return alerting.Watch{}, fmt.Errorf("store: read watch %s: %w", id, err)
	}
	return w, nil
}

// Delete removes a watch. Its state, episodes and history go with it, by cascade:
// an incident that outlives its watch has nothing left that could resolve it.
func (s *Store) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM alert_watches WHERE id = $1::uuid`, id)
	if err != nil {
		return fmt.Errorf("store: delete watch %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return alerting.ErrWatchNotFound
	}
	return nil
}

// List returns every watch, newest first, each with its current state.
func (s *Store) List(ctx context.Context) ([]alerting.Due, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+watchColumns+`, `+stateColumns+`
		  FROM alert_watches AS w
		  LEFT JOIN alert_watch_state AS s ON s.watch_id = w.id
		 ORDER BY w.created_at DESC, w.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("store: list watches: %w", err)
	}
	defer rows.Close()

	var out []alerting.Due
	for rows.Next() {
		var item alerting.Due
		if err := scanWatchAndState(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read the watch list: %w", err)
	}
	return out, nil
}

// Due returns the watches whose next evaluation has come round, oldest due first
// so a tick reads a prefix rather than the table.
//
// Disabled watches are excluded here rather than filtered by the caller: a
// disabled watch is never due, and a scheduler that fetched them would spend its
// per-tick budget on rows it was going to throw away.
func (s *Store) Due(ctx context.Context, now time.Time, limit int) ([]alerting.Due, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+watchColumns+`, `+stateColumns+`
		  FROM alert_watch_state AS s
		  JOIN alert_watches AS w ON w.id = s.watch_id
		 WHERE w.enabled AND s.next_due_at <= $1
		 ORDER BY s.next_due_at
		 LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list due watches: %w", err)
	}
	defer rows.Close()

	var out []alerting.Due
	for rows.Next() {
		var item alerting.Due
		if err := scanWatchAndState(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read due watches: %w", err)
	}
	return out, nil
}

// Count is how many watches exist, for the cap the runner enforces.
func (s *Store) Count(ctx context.Context) (int, error) {
	var n int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM alert_watches`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count watches: %w", err)
	}
	return n, nil
}

// Defer pushes a watch's next evaluation out without recording one, for a watch
// the runner deliberately did not look at. Without it a skipped watch stays due
// and is retried on every tick for as long as the reason lasts.
func (s *Store) Defer(ctx context.Context, watchID string, until time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE alert_watch_state SET next_due_at = $2 WHERE watch_id = $1::uuid`, watchID, until)
	if err != nil {
		return fmt.Errorf("store: defer watch %s: %w", watchID, err)
	}
	return nil
}

// MarkInvalid parks a watch whose stored definition will not build.
//
// It is deferred as well as marked, so a definition this process cannot read does
// not consume a slot on every tick forever. The phase is what the list view shows,
// and it is the honest answer: this watch is not being evaluated.
func (s *Store) MarkInvalid(ctx context.Context, watchID string, until time.Time, reason string) error {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE alert_watch_state
			   SET phase = $2, since = now(), next_due_at = $3, last_status = $4
			 WHERE watch_id = $1::uuid`,
			watchID, alerting.PhaseInvalid, until, string(alerting.StatusError)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO alert_evaluations (watch_id, status, phase, previous_phase, reason, error, detail)
			VALUES ($1::uuid, $2, $3, $3, $4, $5, '[]'::jsonb)`,
			watchID, alerting.StatusError, alerting.PhaseInvalid, alerting.ReasonWatchInvalid, reason)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: mark watch %s invalid: %w", watchID, err)
	}
	return nil
}

// encodeDefinition marshals the two jsonb columns, defaulting each to an empty
// array so a NOT NULL column never receives a JSON null.
func encodeDefinition(w alerting.Watch) (conditions, actions []byte, err error) {
	if conditions, err = json.Marshal(nonNilConditions(w.Conditions)); err != nil {
		return nil, nil, fmt.Errorf("store: encode conditions: %w", err)
	}
	if actions, err = json.Marshal(nonNilActions(w.Actions)); err != nil {
		return nil, nil, fmt.Errorf("store: encode actions: %w", err)
	}
	return conditions, actions, nil
}

func nonNilConditions(in []alerting.ConditionSpec) []alerting.ConditionSpec {
	if in == nil {
		return []alerting.ConditionSpec{}
	}
	return in
}

func nonNilActions(in []alerting.ActionSpec) []alerting.ActionSpec {
	if in == nil {
		return []alerting.ActionSpec{}
	}
	return in
}

func seconds(d time.Duration) int { return int(d.Seconds()) }

// nullableUUID renders a placeholder that casts to uuid only when it is not null,
// because an empty string is not a uuid and NULL is what "nobody" means here.
func nullableUUID(n int) string { return fmt.Sprintf("$%d::uuid", n) }

func nullOrString(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// strip removes the table alias from the shared column list, for the one query
// that returns from an INSERT and so has no alias to qualify with.
func strip(columns string) string {
	out := make([]byte, 0, len(columns))
	for i := 0; i < len(columns); i++ {
		if columns[i] == 'w' && i+1 < len(columns) && columns[i+1] == '.' {
			i++
			continue
		}
		out = append(out, columns[i])
	}
	return string(out)
}

// inTx runs fn inside a transaction, rolling back on any error.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// WithoutCancel because the usual reason a transaction ends early is a
		// cancelled context, and that is exactly when the rollback matters.
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// translate turns the constraint violations this package expects into the
// sentinels callers branch on, and leaves everything else alone.
func translate(err error, what string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return alerting.ErrNameTaken
	}
	return fmt.Errorf("store: %s: %w", what, err)
}
