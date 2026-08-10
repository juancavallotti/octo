package repo

import (
	"context"
	"fmt"
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
