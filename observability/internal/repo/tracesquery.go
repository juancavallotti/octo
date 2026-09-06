package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// TraceApp is one app's trace activity over a window: a row in the list you pick
// from before looking at any individual trace.
//
// An "app" here is a (deployment, name, version) triple rather than a deployment.
// A rollout keeps the deployment id and changes the version, and the version is
// what a cost belongs to — the finer grain collapses into the coarser one on the
// way out, and nothing recovers a version breakdown that was never queried.
type TraceApp struct {
	DeploymentID string `json:"deployment_id"`
	// IntegrationID is empty when the deployment could not be resolved to one.
	IntegrationID string    `json:"integration_id"`
	AppName       string    `json:"app_name"`
	AppVersion    string    `json:"app_version"`
	Traces        int64     `json:"traces"`
	Failed        int64     `json:"failed"`
	LastSeenAt    time.Time `json:"last_seen_at"`

	// CostUSD is what could be priced and UnpricedCalls how many calls could not
	// be. They are reported side by side because the total alone is a lower bound
	// whenever the second is non-zero, and a reader with only the first has no way
	// to know that.
	CostUSD       float64 `json:"cost_usd"`
	UnpricedCalls int64   `json:"unpriced_calls"`

	// DroppedRecords is how many records the publisher admitted losing in this
	// window. It is per app and never per trace: the marker carries no trace id,
	// so nothing can say which traces are incomplete — only that some are.
	DroppedRecords int64 `json:"dropped_records"`
}

// traceAppsSQL aggregates one window of trace activity per app.
//
// The two halves are joined FULL OUTER rather than from the summaries outward. An
// app can produce dropped-record markers in a window where none of its traces
// survived to be summarized, and that is the exact case a reader most needs to
// see — reporting it as "no activity" would hide a loss behind an absence.
const traceAppsSQL = `
WITH traced AS (
    SELECT deployment_id,
           COALESCE(integration_id::text, '') AS integration_id,
           app_name,
           app_version,
           count(*)                                       AS traces,
           count(*) FILTER (WHERE status = 'failed')      AS failed,
           max(ended_at)                                  AS last_seen_at,
           sum(cost_usd)                                  AS cost_usd,
           sum(unpriced_calls)                            AS unpriced_calls
      FROM trace_summaries
     WHERE started_at >= $1 AND started_at <= $2
     GROUP BY deployment_id, integration_id, app_name, app_version
),
dropped AS (
    SELECT deployment_id,
           COALESCE(integration_id::text, '') AS integration_id,
           app_name,
           app_version,
           -- Guarded rather than cast outright: one record whose attrs.dropped is
           -- not a number would fail this query for every app in the window, and
           -- the marker exists precisely to report that something went wrong.
           sum(CASE WHEN jsonb_typeof(attrs -> 'dropped') = 'number'
                    THEN (attrs ->> 'dropped')::bigint ELSE 0 END) AS dropped_records,
           max(ts)                                          AS last_seen_at
      FROM traces
     WHERE kind = 'trace.dropped' AND ts >= $1 AND ts <= $2
     GROUP BY deployment_id, integration_id, app_name, app_version
)
SELECT COALESCE(t.deployment_id,   d.deployment_id)::text AS deployment_id,
       COALESCE(t.integration_id,  d.integration_id)      AS integration_id,
       COALESCE(t.app_name,        d.app_name)            AS app_name,
       COALESCE(t.app_version,     d.app_version)         AS app_version,
       COALESCE(t.traces, 0)                              AS traces,
       COALESCE(t.failed, 0)                              AS failed,
       GREATEST(t.last_seen_at, d.last_seen_at)           AS last_seen_at,
       COALESCE(t.cost_usd, 0)                            AS cost_usd,
       COALESCE(t.unpriced_calls, 0)                      AS unpriced_calls,
       COALESCE(d.dropped_records, 0)                     AS dropped_records
  FROM traced t
  FULL OUTER JOIN dropped d
    ON  t.deployment_id  = d.deployment_id
    AND t.integration_id = d.integration_id
    AND t.app_name       = d.app_name
    AND t.app_version    = d.app_version
 ORDER BY last_seen_at DESC, app_name, app_version`

// Apps returns one row per app with trace activity in [from, to], most recently
// active first.
func (t *Traces) Apps(ctx context.Context, from, to time.Time) ([]TraceApp, error) {
	rows, err := t.pool.Query(ctx, traceAppsSQL, from, to)
	if err != nil {
		return nil, fmt.Errorf("repo: query trace apps: %w", err)
	}
	defer rows.Close()

	var out []TraceApp
	for rows.Next() {
		var app TraceApp
		if err := rows.Scan(
			&app.DeploymentID, &app.IntegrationID, &app.AppName, &app.AppVersion,
			&app.Traces, &app.Failed, &app.LastSeenAt,
			&app.CostUSD, &app.UnpricedCalls, &app.DroppedRecords,
		); err != nil {
			return nil, fmt.Errorf("repo: scan trace app: %w", err)
		}
		out = append(out, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repo: iterate trace apps: %w", err)
	}
	return out, nil
}

// TraceListRow is one trace as the list shows it: the whole summary, because
// every column of it was rolled up to be read here.
type TraceListRow struct {
	TraceID       string `json:"trace_id"`
	DeploymentID  string `json:"deployment_id"`
	IntegrationID string `json:"integration_id"`
	AppName       string `json:"app_name"`
	AppVersion    string `json:"app_version"`

	// DeploymentIDs is every deployment that contributed records. More than one
	// means the trace crossed apps: the id rides on the message and survives a
	// queue hop, so this is how a reader learns the trace is not one app's story.
	DeploymentIDs []string `json:"deployment_ids"`

	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`

	RootFlow   string `json:"root_flow"`
	EntryKind  string `json:"entry_kind"`
	EntryLabel string `json:"entry_label"`

	Status         string `json:"status"`
	RootDurationNs int64  `json:"root_duration_ns"`
	Records        int    `json:"records"`

	LLMCalls      int      `json:"llm_calls"`
	InputTokens   int64    `json:"input_tokens"`
	OutputTokens  int64    `json:"output_tokens"`
	CachedTokens  int64    `json:"cached_tokens"`
	CostUSD       float64  `json:"cost_usd"`
	UnpricedCalls int      `json:"unpriced_calls"`
	Models        []string `json:"models"`
}

// TraceCursor is where a page resumes from.
//
// It is a pair rather than a timestamp because traces tie: a burst of requests
// starts many of them inside the same millisecond, and a cursor on the timestamp
// alone either skips the rows that tie across a page boundary or serves them
// twice. The trace id breaks the tie and the (started_at DESC, trace_id DESC)
// indexes are built to be read this way.
type TraceCursor struct {
	StartedAt time.Time
	TraceID   string
}

// TraceFilter narrows a trace list. A zero field means no constraint on that axis.
type TraceFilter struct {
	DeploymentID  string
	IntegrationID string
	AppName       string
	AppVersion    string
	Flow          string
	Statuses      []string

	// MinDuration filters on ended_at - started_at rather than on the root flow's
	// own measured duration, because the span is defined for every trace: one
	// whose terminal record was lost still has an interval, and silently dropping
	// it from a "slower than" filter would hide the traces most likely to be
	// interesting.
	MinDuration time.Duration
	// HasLLM keeps only traces that made at least one model call.
	HasLLM bool

	Search string
	From   *time.Time
	To     *time.Time
	Before *TraceCursor
	Limit  int
}

// traceListColumns is the projection shared by the list and the detail view's
// summary, so the two cannot disagree about what a trace is.
const traceListColumns = `
    trace_id, deployment_id, COALESCE(integration_id::text, ''), app_name, app_version,
    deployment_ids::text[], started_at, ended_at,
    root_flow, entry_kind, entry_label,
    status, root_duration_ns, records,
    llm_calls, input_tokens, output_tokens, cached_tokens, cost_usd, unpriced_calls, models`

// List returns traces matching f, newest first.
func (t *Traces) List(ctx context.Context, f TraceFilter) ([]TraceListRow, error) {
	var sb strings.Builder
	sb.WriteString("SELECT" + traceListColumns + " FROM trace_summaries")

	var where []string
	var args []any
	add := func(cond string, vals ...any) {
		placeholders := make([]any, len(vals))
		for i, val := range vals {
			args = append(args, val)
			placeholders[i] = len(args)
		}
		where = append(where, fmt.Sprintf(cond, placeholders...))
	}

	if f.DeploymentID != "" {
		add("deployment_id = $%d::uuid", f.DeploymentID)
	}
	if f.IntegrationID != "" {
		add("integration_id = $%d::uuid", f.IntegrationID)
	}
	if f.AppName != "" {
		add("app_name = $%d", f.AppName)
	}
	if f.AppVersion != "" {
		add("app_version = $%d", f.AppVersion)
	}
	if f.Flow != "" {
		add("root_flow = $%d", f.Flow)
	}
	if len(f.Statuses) > 0 {
		add("status = ANY($%d)", f.Statuses)
	}
	if f.MinDuration > 0 {
		add("ended_at - started_at >= $%d", f.MinDuration)
	}
	if f.HasLLM {
		where = append(where, "llm_calls > 0")
	}
	if f.From != nil {
		add("started_at >= $%d", *f.From)
	}
	if f.To != nil {
		add("started_at <= $%d", *f.To)
	}
	if f.Search != "" {
		// Whichever of the three names a reader has in hand: the id they were
		// given, the route they called, or the flow they know.
		add("(trace_id ILIKE '%%' || $%d || '%%' OR entry_label ILIKE '%%' || $%d || '%%'"+
			" OR root_flow ILIKE '%%' || $%d || '%%')", f.Search, f.Search, f.Search)
	}
	if f.Before != nil {
		// Row comparison, not two predicates: (a, b) < (x, y) is the whole keyset
		// condition in the form the composite index can seek to directly.
		add("(started_at, trace_id) < ($%d, $%d)", f.Before.StartedAt, f.Before.TraceID)
	}

	if len(where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(where, " AND "))
	}
	args = append(args, f.Limit)
	fmt.Fprintf(&sb, " ORDER BY started_at DESC, trace_id DESC LIMIT $%d", len(args))

	rows, err := t.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("repo: query traces: %w", err)
	}
	defer rows.Close()

	var out []TraceListRow
	for rows.Next() {
		row, err := scanTraceListRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repo: iterate traces: %w", err)
	}
	return out, nil
}

// scanner is the part of pgx.Rows and pgx.Row a projection needs, so the list and
// the single-trace lookup can share one scan of traceListColumns.
type scanner interface {
	Scan(dest ...any) error
}

func scanTraceListRow(src scanner) (TraceListRow, error) {
	var row TraceListRow
	err := src.Scan(
		&row.TraceID, &row.DeploymentID, &row.IntegrationID, &row.AppName, &row.AppVersion,
		&row.DeploymentIDs, &row.StartedAt, &row.EndedAt,
		&row.RootFlow, &row.EntryKind, &row.EntryLabel,
		&row.Status, &row.RootDurationNs, &row.Records,
		&row.LLMCalls, &row.InputTokens, &row.OutputTokens, &row.CachedTokens,
		&row.CostUSD, &row.UnpricedCalls, &row.Models,
	)
	if err != nil {
		return TraceListRow{}, fmt.Errorf("repo: scan trace: %w", err)
	}
	return row, nil
}

// maxTraceRecords caps one trace's detail response.
//
// A trace that large is pathological — a runaway foreach, or a loop nobody meant
// to write — and it is exactly the trace someone opens to find out why. The cap
// is what keeps opening it from being a second incident; the flag beside it is
// what keeps the answer honest about being partial.
const maxTraceRecords = 20_000

// TraceRecordRow is one stored record, as the detail view reads it.
//
// The token counts and the cost are pointers because their absence is a fact:
// NULL means the provider reported nothing, or the model could not be priced, and
// cost_status says which. A zero here would be a cost of nothing, which is a
// different claim and the one thing this path must never accidentally make.
type TraceRecordRow struct {
	ID            string `json:"id"`
	TraceID       string `json:"trace_id"`
	Seq           int64  `json:"seq"`
	Kind          string `json:"kind"`
	EventID       string `json:"event_id"`
	CorrelationID string `json:"correlation_id"`
	Flow          string `json:"flow"`
	Path          string `json:"path"`
	BlockType     string `json:"block_type"`

	Time       time.Time `json:"ts"`
	DurationNs int64     `json:"duration_ns"`

	Err string `json:"error"`
	// Dropped marks a record the runtime shipped as incomplete; Truncated marks
	// one whose payload was cut to fit.
	Dropped   bool `json:"dropped"`
	Truncated bool `json:"truncated"`

	// Body and Vars are null when the runtime captured no payload AND when the
	// caller asked for the trace without them. The two are indistinguishable here
	// by design: a client that asked for no bodies knows it did.
	// swaggertype says what RawMessage means on the wire: bytes to Go, arbitrary
	// JSON to a client. Without it the description would call these base64 strings.
	// Their shape belongs to the block that was traced, not to this API.
	Body  json.RawMessage `json:"body" swaggertype:"object"`
	Vars  json.RawMessage `json:"vars" swaggertype:"object"`
	Attrs json.RawMessage `json:"attrs" swaggertype:"object"`

	DeploymentID string `json:"deployment_id"`
	AppName      string `json:"app_name"`
	AppVersion   string `json:"app_version"`

	Model          string `json:"model"`
	Provider       string `json:"provider"`
	InputTokens    *int   `json:"input_tokens"`
	OutputTokens   *int   `json:"output_tokens"`
	ThinkingTokens *int   `json:"thinking_tokens"`
	CachedTokens   *int   `json:"cached_tokens"`
	// CacheWriteTokens is cache creation, billed above the input rate. NULL only
	// when the record carried no usage at all (including every record written
	// before the column existed); 0 when a provider reported usage but no write
	// count, which is every provider but Anthropic.
	CacheWriteTokens *int     `json:"cache_write_tokens"`
	CostUSD          *float64 `json:"cost_usd"`
	CostStatus       string   `json:"cost_status"`
	PriceID          string   `json:"price_id"`
}

// traceRecordColumns is the record projection, with the two payload columns left
// to the caller: a waterfall needs every record's shape and almost none of their
// contents, and the bodies are the whole weight of the response.
const traceRecordColumns = `
    id::text, trace_id, seq, kind, event_id, correlation_id, flow, path, block_type,
    ts, duration_ns, error, dropped, truncated,
    %s, %s, attrs,
    deployment_id::text, app_name, app_version,
    model, provider, input_tokens, output_tokens, thinking_tokens, cached_tokens,
    cache_write_tokens, cost_usd, cost_status, COALESCE(price_id::text, '')`

// The two projections, built once so the column order cannot drift between them.
var (
	recordColumnsWithBodies = fmt.Sprintf(traceRecordColumns, "body", "vars")
	recordColumnsNoBodies   = fmt.Sprintf(traceRecordColumns, "NULL::jsonb", "NULL::jsonb")
)

// Trace returns one trace's summary, reporting false when there is no such trace.
func (t *Traces) Trace(ctx context.Context, traceID string) (TraceListRow, bool, error) {
	row, err := scanTraceListRow(t.pool.QueryRow(ctx,
		"SELECT"+traceListColumns+" FROM trace_summaries WHERE trace_id = $1", traceID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TraceListRow{}, false, nil
		}
		return TraceListRow{}, false, err
	}
	return row, true, nil
}

// Records returns one trace's records in publication order, and whether the trace
// held more than the cap.
//
// Ordering by seq rides the (trace_id, seq) index, and it is publication order
// rather than time order on purpose: seq is the only total order a publisher
// gives, whereas two records can share a timestamp.
func (t *Traces) Records(ctx context.Context, traceID string, withBodies bool) ([]TraceRecordRow, bool, error) {
	columns := recordColumnsNoBodies
	if withBodies {
		columns = recordColumnsWithBodies
	}

	// One past the cap, so a full page can be told from a trace that happens to
	// end exactly on it.
	rows, err := t.pool.Query(ctx,
		"SELECT"+columns+" FROM traces WHERE trace_id = $1 ORDER BY seq LIMIT $2",
		traceID, maxTraceRecords+1)
	if err != nil {
		return nil, false, fmt.Errorf("repo: query trace records: %w", err)
	}
	defer rows.Close()

	var out []TraceRecordRow
	for rows.Next() {
		row, err := scanTraceRecordRow(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("repo: iterate trace records: %w", err)
	}

	if len(out) > maxTraceRecords {
		return out[:maxTraceRecords], true, nil
	}
	return out, false, nil
}

// Record returns one record of one trace, with its payloads.
//
// It is addressed under its trace rather than by id alone: a record id is only
// ever handed out alongside the trace it belongs to, so requiring both keeps the
// route from becoming a way to read arbitrary captured payloads by guessing.
func (t *Traces) Record(ctx context.Context, traceID, id string) (TraceRecordRow, bool, error) {
	row, err := scanTraceRecordRow(t.pool.QueryRow(ctx,
		"SELECT"+recordColumnsWithBodies+" FROM traces WHERE trace_id = $1 AND id = $2::uuid",
		traceID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TraceRecordRow{}, false, nil
		}
		return TraceRecordRow{}, false, err
	}
	return row, true, nil
}

func scanTraceRecordRow(src scanner) (TraceRecordRow, error) {
	var row TraceRecordRow
	err := src.Scan(
		&row.ID, &row.TraceID, &row.Seq, &row.Kind, &row.EventID, &row.CorrelationID,
		&row.Flow, &row.Path, &row.BlockType,
		&row.Time, &row.DurationNs, &row.Err, &row.Dropped, &row.Truncated,
		&row.Body, &row.Vars, &row.Attrs,
		&row.DeploymentID, &row.AppName, &row.AppVersion,
		&row.Model, &row.Provider, &row.InputTokens, &row.OutputTokens,
		&row.ThinkingTokens, &row.CachedTokens, &row.CacheWriteTokens,
		&row.CostUSD, &row.CostStatus, &row.PriceID,
	)
	if err != nil {
		return TraceRecordRow{}, fmt.Errorf("repo: scan trace record: %w", err)
	}
	return row, nil
}
