package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/juancavallotti/octo/observability/internal/alerting"
	"github.com/juancavallotti/octo/observability/internal/httpx"
)

const (
	// alertTimeout bounds the CRUD work behind a watch: a statement or two
	// against small tables.
	alertTimeout = 10 * time.Second

	// previewTimeout is longer, because a preview runs a real evaluation — the
	// same fetches a tick would issue, against whatever window the definition
	// asks for.
	previewTimeout = 45 * time.Second

	// maxMuteAhead bounds a mute. A mute with no end is a watch switched off
	// without the list saying so, which is what `enabled` is for.
	maxMuteAhead = 30 * 24 * time.Hour
)

// AlertService is what the handler needs from the alerting service. Declared
// here so the handler tests without a database.
type AlertService interface {
	Create(ctx context.Context, w alerting.Watch, userID string) (alerting.Watch, error)
	Update(ctx context.Context, w alerting.Watch, userID string) (alerting.Watch, error)
	Get(ctx context.Context, id string) (alerting.Watch, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context) ([]alerting.Due, error)
	Preview(ctx context.Context, w alerting.Watch) (alerting.Evaluation, error)
	Mute(ctx context.Context, id string, until time.Time) error
	Acknowledge(ctx context.Context, incidentID, userID string) error
	Incidents(ctx context.Context, f alerting.IncidentFilter) ([]alerting.Incident, error)
	History(ctx context.Context, f alerting.HistoryFilter) ([]alerting.EvaluationRecord, error)
}

// AlertsHandler serves the alerting API.
type AlertsHandler struct {
	svc AlertService
}

// NewAlertsHandler returns a handler backed by svc.
func NewAlertsHandler(svc AlertService) *AlertsHandler {
	return &AlertsHandler{svc: svc}
}

// Register wires the routes onto mux.
//
// These are mutating routes, like retention's and unlike everything else this
// service serves. They live here for the same reason: this service owns the
// tables a watch is stored in and the ones it reads, so a write that hopped
// through the orchestrator would be a hop for its own sake.
func (h *AlertsHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /alerts/watches", h.list)
	mux.HandleFunc("POST /alerts/watches", h.create)
	mux.HandleFunc("GET /alerts/watches/{id}", h.get)
	mux.HandleFunc("PUT /alerts/watches/{id}", h.update)
	mux.HandleFunc("DELETE /alerts/watches/{id}", h.remove)
	mux.HandleFunc("POST /alerts/watches/{id}/mute", h.mute)
	mux.HandleFunc("GET /alerts/watches/{id}/evaluations", h.watchHistory)
	mux.HandleFunc("POST /alerts/preview", h.preview)
	mux.HandleFunc("GET /alerts/evaluations", h.history)
	mux.HandleFunc("GET /alerts/incidents", h.incidents)
	mux.HandleFunc("POST /alerts/incidents/{id}/ack", h.acknowledge)
}

// list returns every watch with its current state.
//
//	@Summary		List watches
//	@Description	Every watch, newest first, each with where its state machine has got to —
//	@Description	the phase it is in, when it last ran, and the incident it currently holds
//	@Description	open. A watch whose stored definition this service cannot read comes back
//	@Description	with phase "invalid" rather than being omitted.
//	@Tags			alerts
//	@Produce		json
//	@Success		200	{object}	watchListResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/alerts/watches [get]
func (h *AlertsHandler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	watches, err := h.svc.List(ctx)
	if err != nil {
		slog.Error("api: list watches", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list watches")
		return
	}
	items := make([]watchListItem, 0, len(watches))
	for _, item := range watches {
		items = append(items, watchListItem{
			Watch: toWatchBody(item.Watch), State: toStateBody(item.State),
		})
	}
	httpx.WriteJSON(w, http.StatusOK, watchListResponse{Items: items})
}

// create stores a new watch.
//
//	@Summary		Create a watch
//	@Description	Validates the whole definition before storing it: every condition is built,
//	@Description	every action is checked, and an unrecognised condition or action type is
//	@Description	refused rather than stored to fail at three in the morning. The watch is due
//	@Description	on the next tick.
//	@Tags			alerts
//	@Accept			json
//	@Produce		json
//	@Param			body	body		watchBody	true	"The watch"
//	@Success		201		{object}	watchBody
//	@Failure		400		{object}	httpx.ErrorResponse	"the definition is not one this service can evaluate"
//	@Failure		409		{object}	httpx.ErrorResponse	"the name is taken, or this installation is at its limit"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/alerts/watches [post]
func (h *AlertsHandler) create(w http.ResponseWriter, r *http.Request) {
	watch, ok := h.decode(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	created, err := h.svc.Create(ctx, watch, userFrom(r))
	if err != nil {
		h.fail(w, "create a watch", err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toWatchBody(created))
}

// get reads one watch.
//
//	@Summary		Read a watch
//	@Tags			alerts
//	@Produce		json
//	@Param			id	path		string	true	"Watch id"
//	@Success		200	{object}	watchBody
//	@Failure		404	{object}	httpx.ErrorResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/alerts/watches/{id} [get]
func (h *AlertsHandler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	watch, err := h.svc.Get(ctx, r.PathValue("id"))
	if err != nil {
		h.fail(w, "read a watch", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWatchBody(watch))
}

// update replaces a definition.
//
//	@Summary		Update a watch
//	@Description	A save replaces the whole definition. Retuning anything that changes what a
//	@Description	pending hold means — a condition, the combinator, the hold itself — restarts
//	@Description	that hold on the next evaluation, because the clock was measuring a different
//	@Description	question. Renaming a watch or changing its recipients does not. Disabling one
//	@Description	closes any episode it has open.
//	@Tags			alerts
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string		true	"Watch id"
//	@Param			body	body		watchBody	true	"The whole watch"
//	@Success		200		{object}	watchBody
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		404		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"the name is taken"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/alerts/watches/{id} [put]
func (h *AlertsHandler) update(w http.ResponseWriter, r *http.Request) {
	watch, ok := h.decode(w, r)
	if !ok {
		return
	}
	// The path wins over the body, so a mismatched id updates what the caller
	// addressed rather than whatever the body happened to carry.
	watch.ID = r.PathValue("id")

	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	updated, err := h.svc.Update(ctx, watch, userFrom(r))
	if err != nil {
		h.fail(w, "update a watch", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toWatchBody(updated))
}

// remove deletes a watch.
//
//	@Summary		Delete a watch
//	@Description	Closes any open episode first, then removes the watch and everything that
//	@Description	referenced it — its state, its incidents and its evaluation history. An
//	@Description	incident that outlived its watch would have nothing left that could resolve it.
//	@Tags			alerts
//	@Param			id	path	string	true	"Watch id"
//	@Success		204	"deleted"
//	@Failure		404	{object}	httpx.ErrorResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/alerts/watches/{id} [delete]
func (h *AlertsHandler) remove(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("id")); err != nil {
		h.fail(w, "delete a watch", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mute suppresses a watch's notifications.
//
//	@Summary		Mute a watch
//	@Description	Suppresses notifications until the given time without stopping evaluation, so
//	@Description	the history stays complete and an open incident still resolves on its own. A
//	@Description	null `until` lifts the mute. Silencing a watch permanently is what `enabled`
//	@Description	is for, so a mute may not be set more than 30 days ahead.
//	@Tags			alerts
//	@Accept			json
//	@Produce		json
//	@Param			id		path	string		true	"Watch id"
//	@Param			body	body	muteRequest	true	"When the mute lifts"
//	@Success		204		"muted"
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		404		{object}	httpx.ErrorResponse
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/alerts/watches/{id}/mute [post]
func (h *AlertsHandler) mute(w http.ResponseWriter, r *http.Request) {
	var req muteRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var until time.Time
	if req.Until != nil {
		until = *req.Until
		if until.After(time.Now().Add(maxMuteAhead)) {
			httpx.WriteError(w, http.StatusBadRequest,
				"a mute may not be set more than 30 days ahead; disable the watch instead")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	if err := h.svc.Mute(ctx, r.PathValue("id"), until); err != nil {
		h.fail(w, "mute a watch", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// preview evaluates a definition without storing anything.
//
//	@Summary		Preview a watch
//	@Description	Runs the definition against real data now and reports what each condition
//	@Description	observed, what it was judged against, and whether it held — without recording
//	@Description	an evaluation, moving any state or notifying anybody. It takes a whole watch
//	@Description	rather than an id, because the question is worth asking about a definition
//	@Description	that has not been saved: it is how a spike's gates get tuned before the watch
//	@Description	goes live rather than after it fails to fire.
//	@Tags			alerts
//	@Accept			json
//	@Produce		json
//	@Param			body	body		watchBody	true	"The definition to try"
//	@Success		200		{object}	previewResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/alerts/preview [post]
func (h *AlertsHandler) preview(w http.ResponseWriter, r *http.Request) {
	watch, ok := h.decode(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), previewTimeout)
	defer cancel()

	evaluation, err := h.svc.Preview(ctx, watch)
	if err != nil {
		h.fail(w, "preview a watch", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, previewResponse{
		Status: string(evaluation.Status), Verdict: evaluation.Verdict.String(),
		Matched: evaluation.Matched, Total: evaluation.Total, Degraded: evaluation.Degraded,
		WindowFrom: evaluation.WindowFrom, WindowTo: evaluation.WindowTo,
		Outcomes: nonNilOutcomes(evaluation.Outcomes),
	})
}

// incidents lists episodes.
//
//	@Summary		List incidents
//	@Description	Firing episodes, most recent first. `open=true` is the "what is on fire right
//	@Description	now" view. A closed episode carries the reason it closed, and "resolved" is
//	@Description	deliberately distinct from "stale": the second means the watch stopped being
//	@Description	able to decide, which is not the same as the metric coming back.
//	@Tags			alerts
//	@Produce		json
//	@Param			watchId	query		string	false	"Only this watch's episodes"
//	@Param			open	query		bool	false	"Only episodes that are still running"
//	@Param			from	query		string	false	"RFC3339 lower bound on when it opened"
//	@Param			to		query		string	false	"RFC3339 upper bound, exclusive"
//	@Param			limit	query		int		false	"Page size (default 100, max 1000)"
//	@Success		200		{object}	incidentListResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/alerts/incidents [get]
func (h *AlertsHandler) incidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filter := alerting.IncidentFilter{
		WatchID:  q.Get("watchId"),
		OpenOnly: q.Get("open") == "true",
		Limit:    parseLimit(q.Get("limit")),
	}
	var err error
	if filter.From, filter.To, err = parseRange(q); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	incidents, err := h.svc.Incidents(ctx, filter)
	if err != nil {
		slog.Error("api: list incidents", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to list incidents")
		return
	}
	items := make([]incidentBody, 0, len(incidents))
	for _, i := range incidents {
		items = append(items, toIncidentBody(i))
	}
	httpx.WriteJSON(w, http.StatusOK, incidentListResponse{Items: items})
}

// acknowledge marks an open incident as seen.
//
//	@Summary		Acknowledge an incident
//	@Description	Records that somebody has seen it. It does not resolve the episode — only the
//	@Description	metric coming back does that — and acknowledging one twice is refused rather
//	@Description	than silently re-stamping whoever got there first.
//	@Tags			alerts
//	@Param			id	path	string	true	"Incident id"
//	@Success		204	"acknowledged"
//	@Failure		404	{object}	httpx.ErrorResponse	"no such open, unacknowledged incident"
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/alerts/incidents/{id}/ack [post]
func (h *AlertsHandler) acknowledge(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	if err := h.svc.Acknowledge(ctx, r.PathValue("id"), userFrom(r)); err != nil {
		h.fail(w, "acknowledge an incident", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// watchHistory pages one watch's evaluations.
//
//	@Summary		A watch's evaluation history
//	@Description	Every time this watch was asked, newest first — including the ticks that found
//	@Description	nothing, which is what makes "it was evaluated and it was fine" distinguishable
//	@Description	from "it was never evaluated". `notable=true` drops the quiet rows.
//	@Tags			alerts
//	@Produce		json
//	@Param			id			path		string	true	"Watch id"
//	@Param			notable		query		bool	false	"Only rows where something happened"
//	@Param			status		query		string	false	"Only this status (repeatable)"
//	@Param			incidentId	query		string	false	"Only rows belonging to this episode"
//	@Param			from		query		string	false	"RFC3339 lower bound"
//	@Param			to			query		string	false	"RFC3339 upper bound, exclusive"
//	@Param			before		query		string	false	"Opaque cursor from a previous page's next_before"
//	@Param			limit		query		int		false	"Page size (default 100, max 1000)"
//	@Success		200			{object}	evaluationListResponse
//	@Failure		400			{object}	httpx.ErrorResponse
//	@Failure		500			{object}	httpx.ErrorResponse
//	@Router			/alerts/watches/{id}/evaluations [get]
func (h *AlertsHandler) watchHistory(w http.ResponseWriter, r *http.Request) {
	h.serveHistory(w, r, r.PathValue("id"))
}

// history pages every watch's evaluations at once.
//
//	@Summary		The whole evaluation log
//	@Description	The same rows as a single watch's history, across every watch — the view that
//	@Description	answers "what has alerting been doing" rather than "what has this watch been
//	@Description	doing". Paged on an opaque cursor naming an (evaluated_at, id) pair, because a
//	@Description	tick evaluates every due watch inside the same millisecond.
//	@Tags			alerts
//	@Produce		json
//	@Param			watchId		query		string	false	"Only this watch"
//	@Param			notable		query		bool	false	"Only rows where something happened"
//	@Param			status		query		string	false	"Only this status (repeatable)"
//	@Param			incidentId	query		string	false	"Only rows belonging to this episode"
//	@Param			from		query		string	false	"RFC3339 lower bound"
//	@Param			to			query		string	false	"RFC3339 upper bound, exclusive"
//	@Param			before		query		string	false	"Opaque cursor from a previous page's next_before"
//	@Param			limit		query		int		false	"Page size (default 100, max 1000)"
//	@Success		200			{object}	evaluationListResponse
//	@Failure		400			{object}	httpx.ErrorResponse
//	@Failure		500			{object}	httpx.ErrorResponse
//	@Router			/alerts/evaluations [get]
func (h *AlertsHandler) history(w http.ResponseWriter, r *http.Request) {
	h.serveHistory(w, r, r.URL.Query().Get("watchId"))
}

func (h *AlertsHandler) serveHistory(w http.ResponseWriter, r *http.Request, watchID string) {
	q := r.URL.Query()
	filter := alerting.HistoryFilter{
		WatchID:     watchID,
		IncidentID:  q.Get("incidentId"),
		NotableOnly: q.Get("notable") == "true",
		Limit:       parseLimit(q.Get("limit")),
	}
	for _, s := range q["status"] {
		filter.Statuses = append(filter.Statuses, alerting.Status(s))
	}
	var err error
	if filter.From, filter.To, err = parseRange(q); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	at, id, ok, err := decodeKeyset(q.Get("before"), "id")
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if ok {
		filter.Before = &alerting.HistoryCursor{At: at, ID: id}
	}

	ctx, cancel := context.WithTimeout(r.Context(), alertTimeout)
	defer cancel()

	rows, err := h.svc.History(ctx, filter)
	if err != nil {
		slog.Error("api: read the evaluation history", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to read the evaluation history")
		return
	}
	items := make([]evaluationBody, 0, len(rows))
	for _, row := range rows {
		items = append(items, toEvaluationBody(row))
	}
	out := evaluationListResponse{Items: items}
	// The cursor is only offered on a full page. Offering one on a short page
	// would invite a request that can only come back empty.
	if len(rows) == filter.Clamp() {
		last := rows[len(rows)-1]
		out.NextBefore = encodeKeyset(last.EvaluatedAt, last.ID)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// decode reads a watch from the body, refusing one this service could not store.
func (h *AlertsHandler) decode(w http.ResponseWriter, r *http.Request) (alerting.Watch, bool) {
	var body watchBody
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return alerting.Watch{}, false
	}
	watch, err := toWatch(body)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "a condition or action is not valid JSON")
		return alerting.Watch{}, false
	}
	return watch, true
}

// fail maps a service error onto a status.
//
// The validation errors go out as 400 with the message the domain wrote, because
// those messages name the field and the bound — "minSamples 9 exceeds the
// 3-bucket window" is the whole of what somebody needs, and replacing it with
// "invalid watch" would throw it away.
func (h *AlertsHandler) fail(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, alerting.ErrWatchNotFound), errors.Is(err, alerting.ErrIncidentNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not found")
	case errors.Is(err, alerting.ErrNameTaken):
		httpx.WriteError(w, http.StatusConflict, "a watch with that name already exists")
	case errors.Is(err, alerting.ErrTooManyWatches):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, alerting.ErrInvalidWatch), errors.Is(err, alerting.ErrInvalidParams),
		errors.Is(err, alerting.ErrUnknownCondition), errors.Is(err, alerting.ErrUnknownAction),
		errors.Is(err, alerting.ErrNestedConditions), errors.Is(err, alerting.ErrUnknownSource):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("api: "+what, "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to "+what)
	}
}

// userFrom reads the acting user from the header the platform's BFF sets.
//
// This service has no auth of its own — the BFF is the authz boundary — so this
// is attribution rather than authorization, and an absent header is simply an
// unattributed change rather than a rejected one.
func userFrom(r *http.Request) string { return r.Header.Get("X-Octo-User-Id") }

func parseLimit(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

// parseRange reads the from/to bounds shared by both list endpoints.
func parseRange(q map[string][]string) (from, to *time.Time, err error) {
	if from, err = parseStamp(first(q, "from"), "from"); err != nil {
		return nil, nil, err
	}
	if to, err = parseStamp(first(q, "to"), "to"); err != nil {
		return nil, nil, err
	}
	return from, to, nil
}

func parseStamp(raw, label string) (*time.Time, error) {
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errInvalid(label + " must be an RFC3339 timestamp")
	}
	return &t, nil
}

func first(q map[string][]string, key string) string {
	if v := q[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}
