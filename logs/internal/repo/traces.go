package repo

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/logs/internal/ingest"
)

// emptyJSONObject is what a record with no attributes is stored as. The column is
// NOT NULL because every record has attributes even when they are none, unlike
// body and vars, where absence means the payload was never captured.
var emptyJSONObject = json.RawMessage(`{}`)

// traceColumns is the insert order for the bulk copy. It is a package variable so
// the column list and the value builder below cannot drift apart silently.
var traceColumns = []string{
	"trace_id", "seq", "kind", "event_id", "correlation_id",
	"flow", "path", "block_type", "ts", "duration_ns",
	"error", "dropped", "truncated", "body", "vars", "attrs",
	"deployment_id", "integration_id", "app_name", "app_version",
	"model", "provider", "input_tokens", "output_tokens", "thinking_tokens",
	"cached_tokens", "cache_write_tokens", "cost_usd", "cost_status", "price_id",
}

// Traces reads and writes the traces table and the per-trace rollup beside it.
type Traces struct {
	pool *pgxpool.Pool
}

// NewTraces returns a trace store backed by pool.
func NewTraces(pool *pgxpool.Pool) *Traces {
	return &Traces{pool: pool}
}

// Insert stores a batch of records and folds them into their traces' summaries.
//
// Both happen in one transaction. The summary is derived entirely from the
// records, so a batch that stored its records without updating the rollup would
// leave a trace whose totals silently understate it — and nothing afterwards
// would know to recompute, because the records look perfectly stored.
//
// Records go in with COPY rather than one INSERT each: a single request through a
// ten-block flow is a couple of dozen records where the same request produces one
// or two log lines, so the per-statement round trip that suits the log path is the
// wrong shape here.
func (t *Traces) Insert(ctx context.Context, rows []ingest.TraceRow) error {
	if len(rows) == 0 {
		return nil
	}

	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("repo: begin trace insert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	source := pgx.CopyFromSlice(len(rows), func(i int) ([]any, error) {
		return traceValues(rows[i]), nil
	})
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"traces"}, traceColumns, source); err != nil {
		return fmt.Errorf("repo: copy trace records: %w", err)
	}

	if err := upsertSummaries(ctx, tx, FoldTraces(rows)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("repo: commit trace insert: %w", err)
	}
	return nil
}

// traceValues renders one row in traceColumns order.
func traceValues(row ingest.TraceRow) []any {
	record := row.Record

	// Absent and empty are different facts for a payload: nil means the runtime
	// captured none, which is not the same as capturing an empty document.
	body := nilIfEmpty(record.Body)
	vars := nilIfEmpty(record.Vars)
	attrs := record.Attrs
	if len(attrs) == 0 {
		attrs = emptyJSONObject
	}

	var inputTokens, outputTokens, thinkingTokens, cachedTokens, cacheWriteTokens *int
	if usage := record.Usage; usage != nil {
		inputTokens = &usage.InputTokens
		outputTokens = &usage.OutputTokens
		thinkingTokens = &usage.ThinkingTokens
		cachedTokens = &usage.CachedTokens
		cacheWriteTokens = &usage.CacheWriteTokens
	}

	// A cost that is not known is stored as NULL, never as zero, and cost_status
	// beside it says which kind of unknown it was.
	var costUSD *float64
	if amount, known := row.Priced.CostUSD(); known {
		costUSD = &amount
	}

	return []any{
		record.TraceID, record.Seq, record.Kind, record.EventID, record.CorrelationID,
		record.Flow, record.Path, record.BlockType, record.Time, record.DurationNs,
		record.Err, record.Dropped, record.Truncated, body, vars, attrs,
		record.DeploymentID, nilIfBlank(row.IntegrationID), record.AppName, record.AppVersion,
		record.Model, row.Priced.Provider, inputTokens, outputTokens, thinkingTokens,
		cachedTokens, cacheWriteTokens, costUSD, string(row.Priced.Status),
		nilIfBlank(row.Priced.PriceID),
	}
}

// upsertSummaries merges each delta into its trace's rollup.
//
// Every assignment is order-independent — a bound, a sum, a ranked maximum, a set
// union — because records arrive out of sequence order and spread across batches.
// An assignment would make the answer depend on which batch happened to land
// last, and the same records replayed in a different order would summarize
// differently.
//
// The identity columns are the subtle ones. They are not merged but *chosen*, by
// comparing how well each batch's best entry record named the trace: a batch that
// saw the source.receive supersedes one that only saw a flow.started, whichever
// arrived first.
func upsertSummaries(ctx context.Context, tx pgx.Tx, deltas []TraceDelta) error {
	if len(deltas) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, delta := range deltas {
		batch.Queue(upsertSummarySQL,
			delta.TraceID, delta.DeploymentID, emptyIfNil(delta.DeploymentIDs), nilIfBlank(delta.IntegrationID),
			delta.AppName, delta.AppVersion, delta.StartedAt, delta.EndedAt,
			delta.RootFlow, delta.RootFlowSeq,
			delta.EntryKind, delta.EntryLabel, delta.EntryRank, delta.EntrySeq,
			delta.Status, delta.RootDurationNs, delta.Records, delta.LLMCalls,
			delta.InputTokens, delta.OutputTokens, delta.CachedTokens,
			delta.CostUSD, delta.UnpricedCalls, emptyIfNil(delta.Models))
	}

	results := tx.SendBatch(ctx, batch)
	if err := results.Close(); err != nil {
		return fmt.Errorf("repo: roll up traces: %w", err)
	}
	return nil
}

// The two verdicts the upsert turns on, each written once and substituted
// wherever it is needed. Postgres has no way to name a computed value inside an
// ON CONFLICT assignment list, so the alternative is repeating a row comparison
// by hand at eight call sites and hoping every copy stays identical.
const (
	// betterEntrySQL asks whether the incoming batch's entry record named the
	// trace better than the stored one did. One row comparison over the same four
	// terms betterEntry compares in summary.go, in the same order — the fold and
	// this merge decide the same question, so a difference between them would make
	// the answer depend on how a trace's records happened to be split into
	// batches.
	betterEntrySQL = `((excluded.entry_rank, excluded.entry_seq, excluded.deployment_id, excluded.entry_label) < ` +
		`(trace_summaries.entry_rank, trace_summaries.entry_seq, ` +
		`trace_summaries.deployment_id, trace_summaries.entry_label))`

	// betterRootFlowSQL asks the same about the flow. It is separate because a
	// batch can carry a better entry record and no flow at all, or the reverse:
	// source records carry no flow, and flow records name no entry point.
	betterRootFlowSQL = `(excluded.root_flow <> '' AND (trace_summaries.root_flow = '' ` +
		`OR (excluded.root_flow_seq, excluded.root_flow) < ` +
		`(trace_summaries.root_flow_seq, trace_summaries.root_flow)))`

	// distinctTextSQL unions two sets, and serves both array columns. Ordered so
	// the stored array is a function of its contents rather than of the order
	// batches happened to arrive in.
	distinctTextSQL = `ARRAY(SELECT DISTINCT unnest(trace_summaries.%[1]s || excluded.%[1]s) ORDER BY 1)`
)

// upsertSummarySQL merges one delta into its trace's rollup. Built once at
// startup so each verdict above appears in the query exactly as written.
var upsertSummarySQL = fmt.Sprintf(`
INSERT INTO trace_summaries (
    trace_id, deployment_id, deployment_ids, integration_id, app_name, app_version,
    started_at, ended_at, root_flow, root_flow_seq,
    entry_kind, entry_label, entry_rank, entry_seq,
    status, root_duration_ns, records, llm_calls,
    input_tokens, output_tokens, cached_tokens, cost_usd, unpriced_calls, models
) VALUES (
    $1, $2::uuid, $3::uuid[], $4::uuid, $5, $6,
    $7, $8, $9, $10,
    $11, $12, $13, $14,
    $15, $16, $17, $18,
    $19, $20, $21, $22, $23, $24
)
ON CONFLICT (trace_id) DO UPDATE SET
    started_at       = LEAST(trace_summaries.started_at, excluded.started_at),
    ended_at         = GREATEST(trace_summaries.ended_at, excluded.ended_at),
    root_duration_ns = GREATEST(trace_summaries.root_duration_ns, excluded.root_duration_ns),

    records        = trace_summaries.records        + excluded.records,
    llm_calls      = trace_summaries.llm_calls      + excluded.llm_calls,
    input_tokens   = trace_summaries.input_tokens   + excluded.input_tokens,
    output_tokens  = trace_summaries.output_tokens  + excluded.output_tokens,
    cached_tokens  = trace_summaries.cached_tokens  + excluded.cached_tokens,
    cost_usd       = trace_summaries.cost_usd       + excluded.cost_usd,
    unpriced_calls = trace_summaries.unpriced_calls + excluded.unpriced_calls,

    status = CASE
        WHEN 'failed'  IN (trace_summaries.status, excluded.status) THEN 'failed'
        WHEN 'dropped' IN (trace_summaries.status, excluded.status) THEN 'dropped'
        ELSE 'ok'
    END,

    models         = %[3]s,
    deployment_ids = %[4]s,

    deployment_id  = CASE WHEN %[1]s THEN excluded.deployment_id  ELSE trace_summaries.deployment_id  END,
    integration_id = CASE WHEN %[1]s THEN excluded.integration_id ELSE trace_summaries.integration_id END,
    app_name       = CASE WHEN %[1]s THEN excluded.app_name       ELSE trace_summaries.app_name       END,
    app_version    = CASE WHEN %[1]s THEN excluded.app_version    ELSE trace_summaries.app_version    END,
    entry_kind     = CASE WHEN %[1]s THEN excluded.entry_kind     ELSE trace_summaries.entry_kind     END,
    entry_label    = CASE WHEN %[1]s THEN excluded.entry_label    ELSE trace_summaries.entry_label    END,
    entry_rank     = CASE WHEN %[1]s THEN excluded.entry_rank     ELSE trace_summaries.entry_rank     END,
    entry_seq      = CASE WHEN %[1]s THEN excluded.entry_seq      ELSE trace_summaries.entry_seq      END,

    root_flow      = CASE WHEN %[2]s THEN excluded.root_flow      ELSE trace_summaries.root_flow      END,
    root_flow_seq  = CASE WHEN %[2]s THEN excluded.root_flow_seq  ELSE trace_summaries.root_flow_seq  END,

    last_seen_at = now()
`,
	betterEntrySQL,
	betterRootFlowSQL,
	fmt.Sprintf(distinctTextSQL, "models"),
	fmt.Sprintf(distinctTextSQL, "deployment_ids"),
)

// nilIfEmpty maps an absent payload to a NULL column rather than to empty bytes,
// which are not valid JSON and would fail the insert.
func nilIfEmpty(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}

// nilIfBlank maps an unset identifier to NULL. The uuid columns cannot hold "".
func nilIfBlank(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// emptyIfNil sends an empty array rather than NULL for the set columns.
//
// A nil Go slice encodes as SQL NULL, and these columns are NOT NULL — so a
// trace with no model calls, which is most of them, would otherwise fail its
// whole batch on the way in.
func emptyIfNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
