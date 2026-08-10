package repo

import (
	"context"
	"fmt"
	"strings"
	"time"
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
           sum(COALESCE((attrs ->> 'dropped')::bigint, 0)) AS dropped_records,
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
