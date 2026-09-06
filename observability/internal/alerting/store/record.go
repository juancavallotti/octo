package store

import (
	"context"
	"encoding/json"

	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/juancavallotti/octo/observability/internal/alerting"
)

// stateColumns is the state half of every joined read, in scanState's order.
const stateColumns = `
    COALESCE(s.phase, 'ok'), s.since, COALESCE(s.consecutive_firing, 0),
    COALESCE(s.consecutive_ok, 0), COALESCE(s.consecutive_errors, 0),
    COALESCE(s.definition_hash, ''), s.last_eval_at, COALESCE(s.last_status, ''),
    s.last_value, s.incident_id, s.last_notified_at, s.muted_until, s.next_due_at`

// scanWatchAndState reads one joined row.
//
// The state half is COALESCEd because the join is a LEFT one: a watch whose state
// row has somehow gone missing must still list, showing a machine at rest, rather
// than failing the whole page.
func scanWatchAndState(rows pgx.Rows, into *Listed) error {
	var w alerting.Watch
	var st alerting.State
	var conditions, actions []byte
	var step, interval, hold, renotify int
	var since, lastEval, lastNotified, mutedUntil, nextDue *time.Time
	var incidentID *string

	err := rows.Scan(
		&w.ID, &w.Name, &w.Description, &w.Enabled, &w.Severity,
		&w.Combinator, &conditions, &actions, &w.OnNoData,
		&step, &interval, &hold, &renotify,
		&w.DefinitionHash, &w.CreatedAt, &w.UpdatedAt,
		&st.Phase, &since, &st.ConsecutiveFiring, &st.ConsecutiveOK, &st.ConsecutiveErrors,
		&st.DefinitionHash, &lastEval, &st.LastStatus, &st.LastValue,
		&incidentID, &lastNotified, &mutedUntil, &nextDue)
	if err != nil {
		return fmt.Errorf("store: scan a watch: %w", err)
	}

	w.Step = time.Duration(step) * time.Second
	w.Interval = time.Duration(interval) * time.Second
	w.For = time.Duration(hold) * time.Second
	w.Renotify = time.Duration(renotify) * time.Second
	if err := json.Unmarshal(conditions, &w.Conditions); err != nil {
		return fmt.Errorf("store: decode the conditions of watch %s: %w", w.ID, err)
	}
	if err := json.Unmarshal(actions, &w.Actions); err != nil {
		return fmt.Errorf("store: decode the actions of watch %s: %w", w.ID, err)
	}

	st.WatchID = w.ID
	st.Since = deref(since)
	st.LastEvalAt = deref(lastEval)
	st.LastNotifiedAt = deref(lastNotified)
	st.MutedUntil = deref(mutedUntil)
	st.NextDueAt = deref(nextDue)
	if incidentID != nil {
		st.IncidentID = *incidentID
	}

	into.Watch, into.State = w, st
	return nil
}

func deref(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// Record writes one evaluation: the history row, the state row and whatever
// happened to the episode, in a single transaction.
//
// Ordering inside it matters. An episode that is opening is inserted first so its
// id can be stamped onto both the state row and the history row; an episode that
// is closing is closed last, so the evaluation that closed it is already counted
// against it.
//
// The state update is guarded on the evaluation time. Leader election makes two
// evaluators the exception rather than the rule, but a lease that has already
// moved is exactly the case where an old decision would otherwise overwrite a new
// one, and the guard costs nothing.
func (s *Store) Record(ctx context.Context, r alerting.Result) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		state := r.State
		for _, action := range r.Actions {
			if action.Kind != alerting.ActionOpen {
				continue
			}
			id, err := openIncident(ctx, tx, r)
			if err != nil {
				return err
			}
			state.IncidentID = id
		}

		if err := writeState(ctx, tx, state, r); err != nil {
			return err
		}
		if err := writeEvaluation(ctx, tx, state, r); err != nil {
			return err
		}
		return closeEpisodes(ctx, tx, r)
	})
}

func openIncident(ctx context.Context, tx pgx.Tx, r alerting.Result) (string, error) {
	outcomes, err := json.Marshal(r.Evaluation.Outcomes)
	if err != nil {
		return "", fmt.Errorf("store: encode the outcomes that opened an incident: %w", err)
	}
	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO alert_incidents (watch_id, opened_at, severity, opened_matched, opened_total, opened_detail)
		VALUES ($1::uuid, $2, $3, $4, $5, $6)
		RETURNING id`,
		r.Watch.ID, r.Evaluation.At, r.Watch.Severity,
		r.Evaluation.Matched, r.Evaluation.Total, outcomes).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("store: open an incident for watch %s: %w", r.Watch.ID, err)
	}
	return id, nil
}

func writeState(ctx context.Context, tx pgx.Tx, state alerting.State, r alerting.Result) error {
	tag, err := tx.Exec(ctx, `
		UPDATE alert_watch_state
		   SET phase = $2, since = $3, consecutive_firing = $4, consecutive_ok = $5,
		       consecutive_errors = $6, definition_hash = $7, last_eval_at = $8,
		       last_status = $9, last_value = $10, incident_id = $11,
		       last_notified_at = $12, next_due_at = $13
		 WHERE watch_id = $1::uuid
		   AND (last_eval_at IS NULL OR last_eval_at < $8)`,
		r.Watch.ID, state.Phase, state.Since, state.ConsecutiveFiring, state.ConsecutiveOK,
		state.ConsecutiveErrors, r.Watch.DefinitionHash, state.LastEvalAt,
		state.LastStatus, state.LastValue, nullOrString(state.IncidentID),
		nullTime(state.LastNotifiedAt), state.NextDueAt)
	if err != nil {
		return fmt.Errorf("store: update the state of watch %s: %w", r.Watch.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return alerting.ErrStaleEvaluation
	}
	return nil
}

func writeEvaluation(ctx context.Context, tx pgx.Tx, state alerting.State, r alerting.Result) error {
	outcomes, err := json.Marshal(r.Evaluation.Outcomes)
	if err != nil {
		return fmt.Errorf("store: encode the outcomes of an evaluation: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO alert_evaluations (watch_id, incident_id, evaluated_at, status, phase,
		                               previous_phase, transitioned, degraded, matched, total,
		                               window_from, window_to, definition_hash, reason, error,
		                               duration_ms, detail)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)`,
		r.Watch.ID, nullOrString(state.IncidentID), r.Evaluation.At, r.Evaluation.Status,
		state.Phase, r.Previous.Phase, r.Transitioned(), r.Evaluation.Degraded,
		r.Evaluation.Matched, r.Evaluation.Total,
		nullTime(r.Evaluation.WindowFrom), nullTime(r.Evaluation.WindowTo),
		r.Watch.DefinitionHash, r.Evaluation.Reason, r.Evaluation.Err,
		int(r.Duration.Milliseconds()), outcomes)
	if err != nil {
		return fmt.Errorf("store: record an evaluation of watch %s: %w", r.Watch.ID, err)
	}
	if state.IncidentID == "" {
		return nil
	}
	_, err = tx.Exec(ctx,
		`UPDATE alert_incidents SET evaluations = evaluations + 1 WHERE id = $1::uuid`, state.IncidentID)
	if err != nil {
		return fmt.Errorf("store: count an evaluation against its incident: %w", err)
	}
	return nil
}

// closeEpisodes ends any episode this evaluation finished. The reason travels
// with the action, so a stale close and a recovery stay distinguishable in the
// row rather than being reconstructed from timestamps later.
func closeEpisodes(ctx context.Context, tx pgx.Tx, r alerting.Result) error {
	for _, action := range r.Actions {
		if action.Kind != alerting.ActionResolve && action.Kind != alerting.ActionClose {
			continue
		}
		if r.Previous.IncidentID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			UPDATE alert_incidents
			   SET resolved_at = $2, closed_reason = $3
			 WHERE id = $1::uuid AND resolved_at IS NULL`,
			r.Previous.IncidentID, action.At, action.Reason); err != nil {
			return fmt.Errorf("store: close incident %s: %w", r.Previous.IncidentID, err)
		}
	}
	return nil
}

// RecordNotification stamps a successful announcement, so the renotify cooldown
// runs from when somebody was actually told rather than from when the state
// machine asked for it.
func (s *Store) RecordNotification(ctx context.Context, watchID, incidentID string, at time.Time) error {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE alert_watch_state SET last_notified_at = $2 WHERE watch_id = $1::uuid`,
			watchID, at); err != nil {
			return err
		}
		if incidentID == "" {
			return nil
		}
		_, err := tx.Exec(ctx,
			`UPDATE alert_incidents SET notifications = notifications + 1 WHERE id = $1::uuid`, incidentID)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: record a notification for watch %s: %w", watchID, err)
	}
	return nil
}

// Retire closes a watch's open episode and returns its machine to rest, for a
// watch being disabled or deleted. Run in the same transaction as the change it
// accompanies, because an episode that outlives its watch can never resolve.
func (s *Store) Retire(ctx context.Context, watchID, reason string, at time.Time) error {
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE alert_incidents SET resolved_at = $2, closed_reason = $3
			 WHERE watch_id = $1::uuid AND resolved_at IS NULL`, watchID, at, reason); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			UPDATE alert_watch_state
			   SET phase = 'ok', since = $2, consecutive_firing = 0, consecutive_ok = 0,
			       consecutive_errors = 0, incident_id = NULL, last_notified_at = NULL
			 WHERE watch_id = $1::uuid`, watchID, at)
		return err
	})
	if err != nil {
		return fmt.Errorf("store: retire watch %s: %w", watchID, err)
	}
	return nil
}

// Mute suppresses notifications until the given time without stopping evaluation,
// which is what somebody silencing a known-broken deployment for an afternoon
// actually wants: the history stays complete and the incident still resolves.
func (s *Store) Mute(ctx context.Context, watchID string, until time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE alert_watch_state SET muted_until = $2 WHERE watch_id = $1::uuid`, watchID, nullTime(until))
	if err != nil {
		return fmt.Errorf("store: mute watch %s: %w", watchID, err)
	}
	return nil
}

// Acknowledge marks an open incident as seen.
func (s *Store) Acknowledge(ctx context.Context, incidentID, userID string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE alert_incidents SET acknowledged_at = $2, acknowledged_by = `+nullableUUID(3)+`
		 WHERE id = $1::uuid AND acknowledged_at IS NULL`, incidentID, at, nullOrString(userID))
	if err != nil {
		return fmt.Errorf("store: acknowledge incident %s: %w", incidentID, err)
	}
	if tag.RowsAffected() == 0 {
		return alerting.ErrIncidentNotFound
	}
	return nil
}

// Incidents lists episodes, most recent first.
func (s *Store) Incidents(ctx context.Context, f alerting.IncidentFilter) ([]alerting.Incident, error) {
	args := []any{}
	var where strings.Builder
	add := func(format string, value any) {
		args = append(args, value)
		fmt.Fprintf(&where, " AND "+format, len(args))
	}
	if f.WatchID != "" {
		add("i.watch_id = $%d::uuid", f.WatchID)
	}
	if f.From != nil {
		add("i.opened_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("i.opened_at < $%d", *f.To)
	}
	if f.OpenOnly {
		where.WriteString(" AND i.resolved_at IS NULL")
	}
	args = append(args, clampLimit(f.Limit))

	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.watch_id, COALESCE(w.name, ''), i.opened_at, i.resolved_at,
		       i.closed_reason, i.severity, i.acknowledged_at, COALESCE(i.acknowledged_by::text, ''),
		       i.opened_matched, i.opened_total, i.opened_detail, i.evaluations, i.notifications
		  FROM alert_incidents AS i
		  LEFT JOIN alert_watches AS w ON w.id = i.watch_id
		 WHERE true`+where.String()+`
		 ORDER BY i.opened_at DESC, i.id DESC
		 LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("store: list incidents: %w", err)
	}
	defer rows.Close()

	var out []alerting.Incident
	for rows.Next() {
		var in alerting.Incident
		var detail []byte
		if err := rows.Scan(&in.ID, &in.WatchID, &in.WatchName, &in.OpenedAt, &in.ResolvedAt,
			&in.ClosedReason, &in.Severity, &in.AcknowledgedAt, &in.AcknowledgedBy,
			&in.OpenedMatched, &in.OpenedTotal, &detail, &in.Evaluations, &in.Notifications); err != nil {
			return nil, fmt.Errorf("store: scan an incident: %w", err)
		}
		if err := json.Unmarshal(detail, &in.OpenedOutcomes); err != nil {
			return nil, fmt.Errorf("store: decode the outcomes of incident %s: %w", in.ID, err)
		}
		out = append(out, in)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read incidents: %w", err)
	}
	return out, nil
}

// History pages the evaluation log, newest first, keyed on (evaluated_at, id).
//
// The composite cursor is not optional: a tick evaluates every due watch inside
// the same millisecond, so a cursor on the timestamp alone would skip or repeat
// rows that tie across a page boundary — exactly as it would on logs.
func (s *Store) History(ctx context.Context, f alerting.HistoryFilter) ([]alerting.EvaluationRecord, error) {
	args := []any{}
	var where strings.Builder
	add := func(format string, value any) {
		args = append(args, value)
		fmt.Fprintf(&where, " AND "+format, len(args))
	}
	if f.WatchID != "" {
		add("e.watch_id = $%d::uuid", f.WatchID)
	}
	if f.IncidentID != "" {
		add("e.incident_id = $%d::uuid", f.IncidentID)
	}
	if len(f.Statuses) > 0 {
		add("e.status = ANY($%d)", statusStrings(f.Statuses))
	}
	if f.NotableOnly {
		where.WriteString(" AND e.status <> 'ok'")
	}
	if f.From != nil {
		add("e.evaluated_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("e.evaluated_at < $%d", *f.To)
	}
	if f.Before != nil {
		args = append(args, f.Before.At, f.Before.ID)
		fmt.Fprintf(&where, " AND (e.evaluated_at, e.id) < ($%d, $%d::uuid)", len(args)-1, len(args))
	}
	args = append(args, f.Clamp())

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.watch_id, COALESCE(e.incident_id::text, ''), e.evaluated_at, e.status,
		       e.phase, e.previous_phase, e.transitioned, e.degraded, e.matched, e.total,
		       e.window_from, e.window_to, e.reason, e.error, e.duration_ms, e.detail
		  FROM alert_evaluations AS e
		 WHERE true`+where.String()+`
		 ORDER BY e.evaluated_at DESC, e.id DESC
		 LIMIT $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, fmt.Errorf("store: read the evaluation history: %w", err)
	}
	defer rows.Close()

	var out []alerting.EvaluationRecord
	for rows.Next() {
		var rec alerting.EvaluationRecord
		var detail []byte
		if err := rows.Scan(&rec.ID, &rec.WatchID, &rec.IncidentID, &rec.EvaluatedAt, &rec.Status,
			&rec.Phase, &rec.PreviousPhase, &rec.Transitioned, &rec.Degraded, &rec.Matched, &rec.Total,
			&rec.WindowFrom, &rec.WindowTo, &rec.Reason, &rec.Err, &rec.DurationMS, &detail); err != nil {
			return nil, fmt.Errorf("store: scan an evaluation: %w", err)
		}
		if err := json.Unmarshal(detail, &rec.Outcomes); err != nil {
			return nil, fmt.Errorf("store: decode the outcomes of evaluation %s: %w", rec.ID, err)
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read evaluations: %w", err)
	}
	return out, nil
}

func statusStrings(in []alerting.Status) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = string(s)
	}
	return out
}

func clampLimit(limit int) int {
	switch {
	case limit <= 0:
		return alerting.DefaultHistoryLimit
	case limit > alerting.MaxHistoryLimit:
		return alerting.MaxHistoryLimit
	default:
		return limit
	}
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
