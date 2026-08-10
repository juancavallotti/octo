// Package repo holds this service's Postgres access, one type per table it owns.
// Each is a thin wrapper over a shared pool with hand-written SQL, mirroring the
// orchestrator's pgxpool-based repositories.
//
// The types are named for what they store rather than for being repositories,
// and their filters and rows carry the table's name for the same reason: this
// package holds several, and a generic name distinguishes none of them.
package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/juancavallotti/octo/logs/internal/ingest"
)

// Logs reads and writes the logs table.
type Logs struct {
	pool *pgxpool.Pool
}

// NewLogs returns a log store backed by pool.
func NewLogs(pool *pgxpool.Pool) *Logs {
	return &Logs{pool: pool}
}

// Insert stores one log event. attrs is passed as text so Postgres casts it into
// the jsonb column; received_at defaults to now() in the schema.
func (r *Logs) Insert(ctx context.Context, e ingest.LogEvent) error {
	attrs := string(e.Attrs)
	if attrs == "" {
		attrs = "{}"
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO logs (deployment_id, app_name, app_version, ts, level, message, attrs)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		e.DeploymentID, e.AppName, e.AppVersion, e.Time, e.Level, e.Message, attrs,
	)
	if err != nil {
		return fmt.Errorf("repo: insert log: %w", err)
	}
	return nil
}

// LogRow is a stored log event returned by Query. JSON tags are snake_case; the
// platform client maps them to camelCase. attrs is passed through as raw JSON.
type LogRow struct {
	ID           string          `json:"id"`
	DeploymentID string          `json:"deployment_id"`
	AppName      string          `json:"app_name"`
	AppVersion   string          `json:"app_version"`
	Time         time.Time       `json:"ts"`
	Level        string          `json:"level"`
	Message      string          `json:"message"`
	Attrs        json.RawMessage `json:"attrs"`
	ReceivedAt   time.Time       `json:"received_at"`
}

// LogFilter narrows a log query. A zero field means "no constraint on this axis".
// Before is the keyset-pagination cursor: results are strictly older than it.
type LogFilter struct {
	DeploymentID string
	AppName      string
	AppVersion   string
	Levels       []string
	From         *time.Time
	To           *time.Time
	Search       string
	Before       *time.Time
	Limit        int
}

// Query returns log rows matching f, newest first. It builds a parameterized
// WHERE from only the set filters so unused axes add no predicates, and orders by
// (ts DESC) to ride the (deployment_id, ts DESC) / (ts DESC) indexes.
func (r *Logs) Query(ctx context.Context, f LogFilter) ([]LogRow, error) {
	var sb strings.Builder
	sb.WriteString(`SELECT id, deployment_id, app_name, app_version, ts, level, message, attrs, received_at
		FROM logs`)

	var where []string
	var args []any
	add := func(cond string, val any) {
		args = append(args, val)
		where = append(where, fmt.Sprintf(cond, len(args)))
	}
	if f.DeploymentID != "" {
		add("deployment_id = $%d::uuid", f.DeploymentID)
	}
	if f.AppName != "" {
		add("app_name = $%d", f.AppName)
	}
	if f.AppVersion != "" {
		add("app_version = $%d", f.AppVersion)
	}
	if len(f.Levels) > 0 {
		add("level = ANY($%d)", f.Levels)
	}
	if f.From != nil {
		add("ts >= $%d", *f.From)
	}
	if f.To != nil {
		add("ts <= $%d", *f.To)
	}
	if f.Search != "" {
		add("message ILIKE '%%' || $%d || '%%'", f.Search)
	}
	if f.Before != nil {
		add("ts < $%d", *f.Before)
	}
	if len(where) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(where, " AND "))
	}
	args = append(args, f.Limit)
	fmt.Fprintf(&sb, " ORDER BY ts DESC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("repo: query logs: %w", err)
	}
	defer rows.Close()

	var out []LogRow
	for rows.Next() {
		var row LogRow
		if err := rows.Scan(
			&row.ID, &row.DeploymentID, &row.AppName, &row.AppVersion,
			&row.Time, &row.Level, &row.Message, &row.Attrs, &row.ReceivedAt,
		); err != nil {
			return nil, fmt.Errorf("repo: scan log row: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("repo: iterate log rows: %w", err)
	}
	return out, nil
}
