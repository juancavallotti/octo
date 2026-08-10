// Package api exposes the read-only HTTP query surface over what this service
// stores: log events, and the traces beside them. It owns no storage; it parses
// request filters, delegates to a querier, and shapes the JSON the platform's
// views consume.
//
// Types are named for the stream they serve, the same way repo's are. The
// package holds a handler per stream, so a bare "Handler" would name neither.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/juancavallotti/octo/logs/internal/repo"
)

const (
	// defaultLimit/maxLimit bound a page so a query can neither return everything
	// nor be asked to. A caller that omits limit gets defaultLimit.
	defaultLimit = 200
	maxLimit     = 1000
)

// LogQuerier returns log rows matching a filter. The repo implements it; the handler
// depends on the interface so it can be tested without a database.
type LogQuerier interface {
	Query(ctx context.Context, f repo.LogFilter) ([]repo.LogRow, error)
}

// LogsHandler serves the log query API.
type LogsHandler struct {
	q LogQuerier
}

// NewLogsHandler returns a handler backed by q.
func NewLogsHandler(q LogQuerier) *LogsHandler {
	return &LogsHandler{q: q}
}

// Register wires the routes onto mux.
func (h *LogsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /logs", h.list)
}

// logListResponse is one page of log rows, newest first. NextBefore is the cursor to
// pass back as ?before= for the following page; it is omitted on the last page.
type logListResponse struct {
	Items      []repo.LogRow `json:"items"`
	NextBefore *time.Time    `json:"next_before,omitempty"`
}

// list parses the filter from the query string, runs the query, and returns a
// page. A full page (len == limit) carries a next_before cursor (the oldest row's
// timestamp), since more rows may exist beyond it.
func (h *LogsHandler) list(w http.ResponseWriter, r *http.Request) {
	f, err := parseLogFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := h.q.Query(r.Context(), f)
	if err != nil {
		slog.Error("api: query logs", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to query logs")
		return
	}
	if rows == nil {
		rows = []repo.LogRow{}
	}

	resp := logListResponse{Items: rows}
	if len(rows) == f.Limit {
		resp.NextBefore = &rows[len(rows)-1].Time
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseLogFilter builds a repo.LogFilter from the request query parameters, validating
// the typed ones (times, limit) and clamping limit to [1, maxLimit].
func parseLogFilter(r *http.Request) (repo.LogFilter, error) {
	query := r.URL.Query()
	f := repo.LogFilter{
		DeploymentID: query.Get("deploymentId"),
		AppName:      query.Get("appName"),
		AppVersion:   query.Get("appVersion"),
		Levels:       query["level"],
		Search:       query.Get("q"),
		Limit:        defaultLimit,
	}

	from, err := parseTime(query.Get("from"))
	if err != nil {
		return repo.LogFilter{}, err
	}
	f.From = from

	to, err := parseTime(query.Get("to"))
	if err != nil {
		return repo.LogFilter{}, err
	}
	f.To = to

	before, err := parseTime(query.Get("before"))
	if err != nil {
		return repo.LogFilter{}, err
	}
	f.Before = before

	if raw := query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return repo.LogFilter{}, errInvalid("limit must be an integer")
		}
		f.Limit = n
	}
	switch {
	case f.Limit < 1:
		f.Limit = 1
	case f.Limit > maxLimit:
		f.Limit = maxLimit
	}
	return f, nil
}

// parseTime parses an RFC3339 timestamp, returning nil for an empty value so the
// corresponding filter axis stays unconstrained.
func parseTime(raw string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errInvalid("time must be RFC3339: " + raw)
	}
	return &t, nil
}

// errInvalid is a small typed error so handlers can map parse failures to 400.
type errInvalid string

func (e errInvalid) Error() string { return string(e) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("api: encode response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
