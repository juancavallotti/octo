package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/logs/internal/repo"
)

// defaultWindow is how far back an unbounded trace query reaches. Traces are
// stored without retention today, so a query with no window would widen with the
// age of the deployment rather than with what anyone wanted to look at.
const defaultWindow = 24 * time.Hour

// TraceQuerier reads stored traces. The repo implements it; the handler depends
// on the interface so it can be tested without a database.
type TraceQuerier interface {
	Apps(ctx context.Context, from, to time.Time) ([]repo.TraceApp, error)
}

// TracesHandler serves the trace query API.
type TracesHandler struct {
	q TraceQuerier
	// now is the clock the default window is measured from, replaced in tests.
	now func() time.Time
}

// NewTracesHandler returns a handler backed by q.
func NewTracesHandler(q TraceQuerier) *TracesHandler {
	return &TracesHandler{q: q, now: time.Now}
}

// Register wires the routes onto mux.
func (h *TracesHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /traces/apps", h.apps)
}

// appsResponse is the list of apps with trace activity, plus the window it was
// measured over. The window is echoed back because the caller may not have
// chosen it, and every count in the response is meaningless without it.
type appsResponse struct {
	Items []repo.TraceApp `json:"items"`
	From  time.Time       `json:"from"`
	To    time.Time       `json:"to"`
}

// apps serves the app list the trace view is navigated from.
func (h *TracesHandler) apps(w http.ResponseWriter, r *http.Request) {
	from, to, err := h.window(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	apps, err := h.q.Apps(r.Context(), from, to)
	if err != nil {
		slog.Error("api: query trace apps", "err", err)
		writeError(w, http.StatusInternalServerError, "failed to query traces")
		return
	}
	if apps == nil {
		apps = []repo.TraceApp{}
	}
	writeJSON(w, http.StatusOK, appsResponse{Items: apps, From: from, To: to})
}

// window reads the from/to pair, defaulting to the last defaultWindow.
//
// Either bound may be given alone: from with no to reads to now, and to with no
// from reads back one window from there, so "everything since noon" and
// "the day before the incident" both work without spelling out both ends.
func (h *TracesHandler) window(r *http.Request) (time.Time, time.Time, error) {
	query := r.URL.Query()

	from, err := parseTime(query.Get("from"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parseTime(query.Get("to"))
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	switch {
	case from != nil && to != nil:
	case from != nil:
		end := h.now()
		to = &end
	case to != nil:
		start := to.Add(-defaultWindow)
		from = &start
	default:
		end := h.now()
		start := end.Add(-defaultWindow)
		from, to = &start, &end
	}

	if to.Before(*from) {
		return time.Time{}, time.Time{}, errInvalid("to must not be before from")
	}
	return *from, *to, nil
}
