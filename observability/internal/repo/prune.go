package repo

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
)

// Every delete here is issued in bounded batches and looped until a statement
// removes fewer rows than it asked for. A single unbounded DELETE would be
// simpler and is the wrong shape: these tables are the ones that grow without
// limit, the first sweep after the policy is switched on can be the whole of
// them, and one statement holding write locks across every matching row would
// contend with the COPY-based ingest path for as long as it took.
//
// The loops are also the reason a partial sweep is harmless. Each batch commits
// on its own, so an interrupted run has deleted a prefix of what it would have
// and the next run continues from there — there is no state to resume from
// beyond the rows themselves.

// Prune deletes log events stamped before cutoff and returns how many went.
//
// The subselect is what keeps the batch bounded: DELETE takes no LIMIT of its
// own. Ordering it oldest-first is not required for correctness but makes an
// interrupted sweep leave the newest data behind, which is the half worth
// keeping.
func (r *Logs) Prune(ctx context.Context, cutoff time.Time, batch int) (int64, error) {
	var total int64
	for {
		tag, err := r.pool.Exec(ctx,
			`DELETE FROM logs WHERE id IN (
			   SELECT id FROM logs WHERE ts < $1 ORDER BY ts LIMIT $2
			 )`, cutoff, batch)
		if err != nil {
			return total, fmt.Errorf("repo: prune logs: %w", err)
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < int64(batch) {
			return total, nil
		}
	}
}

// TracePruneResult reports what a trace sweep removed. Records and summaries are
// counted apart because they are two tables with two different reasons to shrink,
// and a run that deleted summaries but no records means something quite specific
// went wrong.
type TracePruneResult struct {
	Records   int64
	Summaries int64
}

// Prune deletes traces that started before cutoff, along with their records, and
// then collects records no summary will ever claim.
//
// Two passes, because traces and trace_summaries are on different clocks:
// started_at is MIN(ts − duration_ns) across the trace, so it is at or before
// every one of its records' ts. Sweeping each table on its own timestamp would
// therefore delete a summary while leaving records behind it — and since
// GET /traces/{traceId} serves the *stored* summary rather than recomputing one,
// that trace would stop being readable rather than merely look stale.
//
// So the summary is the unit of retention: a trace goes whole or not at all.
//
//  1. By trace. Take a batch of summaries older than the cutoff, delete their
//     records and then the summaries themselves, in one transaction.
//  2. Orphans. Delete records older than the cutoff that no summary claims.
//
// The second pass is not belt-and-braces. A trace.dropped marker carries no
// trace id — it describes a publisher's stream, not any one run — and FoldTraces
// skips records with an empty trace id, so a marker never produces a summary row
// and pass 1 can never reach it. Without pass 2 those markers accumulate for the
// life of the installation.
//
// It is also cheap, because of the clock relationship above: for a trace whose
// summary survives the cutoff, every record satisfies ts ≥ started_at ≥ cutoff.
// Once pass 1 has finished, the only rows left below the cutoff are the orphans
// pass 2 is looking for.
func (t *Traces) Prune(ctx context.Context, cutoff time.Time, batch int) (TracePruneResult, error) {
	var out TracePruneResult

	for {
		result, err := t.pruneTraceBatch(ctx, cutoff, batch)
		if err != nil {
			return out, err
		}
		out.Records += result.Records
		out.Summaries += result.Summaries
		if result.Summaries < int64(batch) {
			break
		}
	}

	for {
		tag, err := t.pool.Exec(ctx,
			`DELETE FROM traces WHERE id IN (
			   SELECT r.id FROM traces r
			   WHERE r.ts < $1
			     AND NOT EXISTS (
			       SELECT 1 FROM trace_summaries s WHERE s.trace_id = r.trace_id
			     )
			   ORDER BY r.ts
			   LIMIT $2
			 )`, cutoff, batch)
		if err != nil {
			return out, fmt.Errorf("repo: prune orphaned trace records: %w", err)
		}
		out.Records += tag.RowsAffected()
		if tag.RowsAffected() < int64(batch) {
			return out, nil
		}
	}
}

// pruneTraceBatch removes one batch of expired traces, records first and summary
// second, in a single transaction.
//
// The order within the transaction is deliberate even though it commits
// atomically: deleting the summary first would, if this were ever split apart,
// leave records that pass 2 collects, whereas the reverse leaves a summary
// pointing at nothing — a trace that lists but cannot be opened. The transaction
// makes the question moot today and the order keeps it moot if it stops being
// one transaction tomorrow.
func (t *Traces) pruneTraceBatch(ctx context.Context, cutoff time.Time, batch int) (TracePruneResult, error) {
	var out TracePruneResult

	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return out, fmt.Errorf("repo: begin trace prune: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// A trace still receiving records can be selected here if it started before
	// the cutoff, and later records would then rebuild a partial summary for it.
	// With a window measured in days that means a run older than the retention
	// period is still in flight, which is a broken flow rather than a case worth
	// designing around — and the partial summary it leaves expires on its own.
	rows, err := tx.Query(ctx,
		`SELECT trace_id FROM trace_summaries
		 WHERE started_at < $1 ORDER BY started_at LIMIT $2`, cutoff, batch)
	if err != nil {
		return out, fmt.Errorf("repo: select expired traces: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return out, fmt.Errorf("repo: scan expired trace ids: %w", err)
	}
	if len(ids) == 0 {
		return out, nil
	}

	// Sorted so this takes its row locks in trace-id order, which is the order
	// the ingest path issues its summary upserts in. Postgres chooses the actual
	// order, so this is intent rather than a guarantee — a sweep that does
	// deadlock against ingest fails the run and the next one picks the rows up.
	slices.Sort(ids)

	tag, err := tx.Exec(ctx, `DELETE FROM traces WHERE trace_id = ANY($1)`, ids)
	if err != nil {
		return out, fmt.Errorf("repo: delete expired trace records: %w", err)
	}
	out.Records = tag.RowsAffected()

	tag, err = tx.Exec(ctx, `DELETE FROM trace_summaries WHERE trace_id = ANY($1)`, ids)
	if err != nil {
		return out, fmt.Errorf("repo: delete expired trace summaries: %w", err)
	}
	out.Summaries = tag.RowsAffected()

	if err := tx.Commit(ctx); err != nil {
		return out, fmt.Errorf("repo: commit trace prune: %w", err)
	}
	return out, nil
}
