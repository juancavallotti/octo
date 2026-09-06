package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/juancavallotti/octo/observability/internal/httpx"
	"github.com/juancavallotti/octo/observability/internal/repo"
)

// defaultWindow is how far back an unbounded trace query reaches. A query with
// no window would otherwise widen with however much history is stored rather
// than with what anyone wanted to look at — and retention only bounds that at
// whatever the policy says, which may be years and defaults to keeping
// everything.
const defaultWindow = 24 * time.Hour

// TraceQuerier reads stored traces. The repo implements it; the handler depends
// on the interface so it can be tested without a database.
type TraceQuerier interface {
	Apps(ctx context.Context, from, to time.Time) ([]repo.TraceApp, error)
	List(ctx context.Context, f repo.TraceFilter) ([]repo.TraceListRow, error)
	Trace(ctx context.Context, traceID string) (repo.TraceListRow, bool, error)
	Records(ctx context.Context, traceID string, withBodies bool) ([]repo.TraceRecordRow, bool, error)
	Record(ctx context.Context, traceID, id string) (repo.TraceRecordRow, bool, error)
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
	// /traces/apps is registered as a literal, so it wins over the {traceId}
	// pattern for that one path — which is what makes a trace literally named
	// "apps" unreachable, and why nothing generates one.
	mux.HandleFunc("GET /traces/apps", h.apps)
	mux.HandleFunc("GET /traces", h.list)
	mux.HandleFunc("GET /traces/{traceId}", h.detail)
	mux.HandleFunc("GET /traces/{traceId}/records/{id}", h.record)
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
//
//	@Summary		List apps with trace activity
//	@Description	Every app that ran a trace in the window, with its counts and cost. Read this
//	@Description	first to find the app worth listing traces for.
//	@Description
//	@Description	The window is echoed back in the response, because the caller may not have
//	@Description	chosen it and every count is meaningless without it. Either bound may be given
//	@Description	alone: from with no to reads to now, and to with no from reads back 24 hours.
//	@Tags			traces
//	@Produce		json
//	@Param			from	query		string	false	"Start of the window, RFC3339. Defaults to 24h before to"
//	@Param			to		query		string	false	"End of the window, RFC3339. Defaults to now"
//	@Success		200		{object}	appsResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"a time is not RFC3339, or to is before from"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/traces/apps [get]
func (h *TracesHandler) apps(w http.ResponseWriter, r *http.Request) {
	from, to, err := h.window(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	apps, err := h.q.Apps(r.Context(), from, to)
	if err != nil {
		slog.Error("api: query trace apps", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to query traces")
		return
	}
	if apps == nil {
		apps = []repo.TraceApp{}
	}
	httpx.WriteJSON(w, http.StatusOK, appsResponse{Items: apps, From: from, To: to})
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

// traceListResponse is one page of traces, newest first. NextBefore is the cursor
// to pass back as ?before= for the following page, omitted on the last page.
type traceListResponse struct {
	Items      []repo.TraceListRow `json:"items"`
	NextBefore string              `json:"next_before,omitempty"`
}

// list serves one page of traces.
//
// A full page carries a cursor, since more rows may exist beyond it. The cursor
// is the last row's (started_at, trace_id) pair rather than its timestamp alone:
// traces routinely start inside the same millisecond, and a timestamp-only cursor
// either skips the rows that tie across the boundary or serves them twice.
//
//	@Summary		List traces
//	@Description	One page of trace summaries, newest first. Every filter is optional and they
//	@Description	combine; a page that came back full carries next_before, which is the cursor
//	@Description	to send as before= for the page after it.
//	@Description
//	@Description	Unlike /traces/apps neither time bound is defaulted, so an unfiltered call
//	@Description	reaches back as far as the store goes.
//	@Tags			traces
//	@Produce		json
//	@Param			deploymentId	query		string		false	"Only this deployment's traces; must be a uuid"
//	@Param			integrationId	query		string		false	"Only this integration's traces; must be a uuid"
//	@Param			appName			query		string		false	"Only this app's traces"
//	@Param			appVersion		query		string		false	"Only this version's traces"
//	@Param			flow			query		string		false	"Only traces whose root flow is this one"
//	@Param			status			query		[]string	false	"Only these outcomes; repeat the parameter for more than one"	Enums(ok, dropped, failed)	collectionFormat(multi)
//	@Param			hasLlm			query		string		false	"Pass true for only the traces that called a model"
//	@Param			q				query		string		false	"Free-text search"
//	@Param			minDurationMs	query		int			false	"Only traces at least this long, in milliseconds"
//	@Param			from			query		string		false	"Oldest trace to return, RFC3339"
//	@Param			to				query		string		false	"Newest trace to return, RFC3339"
//	@Param			before			query		string		false	"A previous page's next_before. Opaque — it names a (started_at, trace_id) pair"
//	@Param			limit			query		int			false	"Page size, clamped to 1..1000"	default(200)
//	@Success		200				{object}	traceListResponse
//	@Failure		400				{object}	httpx.ErrorResponse	"an id is not a uuid, a status is not one of ok/dropped/failed, a time is not RFC3339, to is before from, or a number is malformed"
//	@Failure		500				{object}	httpx.ErrorResponse
//	@Router			/traces [get]
func (h *TracesHandler) list(w http.ResponseWriter, r *http.Request) {
	f, err := parseTraceFilter(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	rows, err := h.q.List(r.Context(), f)
	if err != nil {
		slog.Error("api: query traces", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to query traces")
		return
	}
	if rows == nil {
		rows = []repo.TraceListRow{}
	}

	resp := traceListResponse{Items: rows}
	if len(rows) == f.Limit {
		last := rows[len(rows)-1]
		resp.NextBefore = encodeCursor(repo.TraceCursor{StartedAt: last.StartedAt, TraceID: last.TraceID})
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

// parseTraceFilter builds a repo.TraceFilter from the query string.
func parseTraceFilter(r *http.Request) (repo.TraceFilter, error) {
	query := r.URL.Query()
	f := repo.TraceFilter{
		DeploymentID:  query.Get("deploymentId"),
		IntegrationID: query.Get("integrationId"),
		AppName:       query.Get("appName"),
		AppVersion:    query.Get("appVersion"),
		Flow:          query.Get("flow"),
		Statuses:      query["status"],
		HasLLM:        query.Get("hasLlm") == "true",
		Search:        query.Get("q"),
		Limit:         defaultLimit,
	}

	if err := validateStatuses(f.Statuses); err != nil {
		return repo.TraceFilter{}, err
	}
	for name, value := range map[string]string{"deploymentId": f.DeploymentID, "integrationId": f.IntegrationID} {
		if err := validateUUID(name, value); err != nil {
			return repo.TraceFilter{}, err
		}
	}

	from, err := parseTime(query.Get("from"))
	if err != nil {
		return repo.TraceFilter{}, err
	}
	f.From = from

	to, err := parseTime(query.Get("to"))
	if err != nil {
		return repo.TraceFilter{}, err
	}
	f.To = to

	// Both bounds are optional here, unlike /traces/apps, but a pair that runs
	// backwards is still a mistake rather than a query: it matches nothing, and an
	// empty page reads exactly like "this app ran no traces".
	if f.From != nil && f.To != nil && f.To.Before(*f.From) {
		return repo.TraceFilter{}, errInvalid("to must not be before from")
	}

	before, err := decodeCursor(query.Get("before"))
	if err != nil {
		return repo.TraceFilter{}, err
	}
	f.Before = before

	if raw := query.Get("minDurationMs"); raw != "" {
		millis, err := strconv.Atoi(raw)
		if err != nil {
			return repo.TraceFilter{}, errInvalid("minDurationMs must be an integer")
		}
		if millis < 0 {
			return repo.TraceFilter{}, errInvalid("minDurationMs must not be negative")
		}
		f.MinDuration = time.Duration(millis) * time.Millisecond
	}

	if raw := query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return repo.TraceFilter{}, errInvalid("limit must be an integer")
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

// validateStatuses rejects a status nothing can ever match.
//
// Passing it through would return an empty page, which reads exactly like "this
// app had no failures" — the answer a typo is most likely to be mistaken for.
func validateStatuses(statuses []string) error {
	for _, status := range statuses {
		switch status {
		case repo.StatusOK, repo.StatusDropped, repo.StatusFailed:
		default:
			return errInvalid("status must be one of ok/dropped/failed: " + status)
		}
	}
	return nil
}

// validateUUID rejects a malformed identifier before it reaches SQL.
//
// These reach the query as ::uuid casts, so Postgres rejects a malformed one
// with an error — which would surface as a 500, blaming the server for a value
// the caller typed. An empty value is no filter at all and passes.
func validateUUID(name, value string) error {
	if value == "" || isUUID(value) {
		return nil
	}
	return errInvalid(name + " must be a uuid: " + value)
}

// isUUID reports whether value has the canonical 8-4-4-4-12 hex shape.
func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, c := range value {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// encodeCursor renders a trace page position, and decodeCursor parses one. Both
// are the shared keyset codec with the trace id as the tie-breaker; see cursor.go.
func encodeCursor(c repo.TraceCursor) string {
	return encodeKeyset(c.StartedAt, c.TraceID)
}

// decodeCursor returns nil for an empty cursor so the first page needs no
// special case.
func decodeCursor(raw string) (*repo.TraceCursor, error) {
	startedAt, traceID, ok, err := decodeKeyset(raw, "traceId")
	if err != nil || !ok {
		return nil, err
	}
	return &repo.TraceCursor{StartedAt: startedAt, TraceID: traceID}, nil
}

// traceDetailResponse is one trace: the rollup the list already showed, plus
// every record that fed it.
//
// The summary is served from the stored rollup rather than recomputed from the
// records below it. That is not an optimization — it is what makes the number in
// the list and the number in the panel the same number, instead of two
// implementations that agree until one of them changes.
type traceDetailResponse struct {
	Summary repo.TraceListRow     `json:"summary"`
	Items   []repo.TraceRecordRow `json:"items"`
	// Truncated says the trace held more records than one response carries. It is
	// reported rather than silently applied, because a waterfall drawn from a cut
	// record set is wrong in a way nothing in the picture reveals.
	Truncated bool `json:"truncated"`
}

// detail serves one trace and its records.
//
//	@Summary		Read one trace
//	@Description	One trace: the summary the list already showed, plus every record that fed
//	@Description	it, in sequence. The summary is the stored rollup rather than a recomputation,
//	@Description	so the number here and the number in the list are the same number.
//	@Description
//	@Description	Captured payloads are the whole weight of a trace. A client drawing only a
//	@Description	waterfall should pass bodies=0 and leave them on the server, then fetch the one
//	@Description	record it needs from /traces/{traceId}/records/{id}.
//	@Tags			traces
//	@Produce		json
//	@Param			traceId	path		string	true	"The trace id"
//	@Param			bodies	query		string	false	"Pass 0 or false to omit the captured payloads. Defaults to including them"
//	@Success		200		{object}	traceDetailResponse
//	@Failure		404		{object}	httpx.ErrorResponse	"no such trace"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/traces/{traceId} [get]
func (h *TracesHandler) detail(w http.ResponseWriter, r *http.Request) {
	traceID := r.PathValue("traceId")

	summary, found, err := h.q.Trace(r.Context(), traceID)
	if err != nil {
		slog.Error("api: query trace", "trace", traceID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to query the trace")
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "no such trace")
		return
	}

	records, truncated, err := h.q.Records(r.Context(), traceID, wantsBodies(r))
	if err != nil {
		slog.Error("api: query trace records", "trace", traceID, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to query the trace")
		return
	}
	if records == nil {
		records = []repo.TraceRecordRow{}
	}

	httpx.WriteJSON(w, http.StatusOK, traceDetailResponse{Summary: summary, Items: records, Truncated: truncated})
}

// record serves one record's captured payloads, for a client that loaded the
// trace without them and then opened a single block.
//
//	@Summary		Read one trace record
//	@Description	One record with its captured payloads, for a client that loaded the trace
//	@Description	with bodies=0 and then opened a single block.
//	@Description
//	@Description	A malformed id answers 404 rather than 400: it addresses no record either
//	@Description	way, and answering alike for "malformed" and "absent" keeps the route from
//	@Description	confirming which ids exist.
//	@Tags			traces
//	@Produce		json
//	@Param			traceId	path		string	true	"The trace id"
//	@Param			id		path		string	true	"The record id, a uuid"
//	@Success		200		{object}	repo.TraceRecordRow
//	@Failure		404		{object}	httpx.ErrorResponse	"no such record, or the id is not a uuid"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/traces/{traceId}/records/{id} [get]
func (h *TracesHandler) record(w http.ResponseWriter, r *http.Request) {
	traceID, id := r.PathValue("traceId"), r.PathValue("id")

	// A malformed id addresses no record, which is what 404 says. It is also a
	// ::uuid cast away from a 500, and answering the same way for "malformed" and
	// "absent" keeps the route from confirming which ids exist.
	if !isUUID(id) {
		httpx.WriteError(w, http.StatusNotFound, "no such record")
		return
	}

	row, found, err := h.q.Record(r.Context(), traceID, id)
	if err != nil {
		slog.Error("api: query trace record", "trace", traceID, "record", id, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to query the record")
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "no such record")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, row)
}

// wantsBodies reads ?bodies, which defaults to on.
//
// Defaulting to on keeps a plain fetch complete; a client that knows it is only
// drawing a waterfall passes bodies=0 and leaves the captured payloads — which
// are the whole weight of a trace — on the server.
func wantsBodies(r *http.Request) bool {
	switch r.URL.Query().Get("bodies") {
	case "0", "false":
		return false
	default:
		return true
	}
}
