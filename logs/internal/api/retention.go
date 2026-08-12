package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/logs/internal/httpx"
	"github.com/juancavallotti/octo/logs/internal/retention"
)

const (
	// settingsTimeout bounds the database work behind reading or writing the
	// policy — two statements against a one-row table.
	settingsTimeout = 5 * time.Second

	// sweepTimeout bounds a purge. It is generous because the first sweep after
	// the policy is switched on can be the whole of both tables, and stingy
	// enough that a sweep which has stopped making progress does not hold its
	// lock forever. The chart's job gives curl the same budget, so neither side
	// gives up on the other.
	sweepTimeout = 15 * time.Minute
)

// RetentionService is what the handler needs from the retention package. The
// handler depends on the interface so it can be tested without a database.
type RetentionService interface {
	Get(ctx context.Context) (retention.Policy, error)
	Update(ctx context.Context, u retention.Update) (retention.Policy, error)
	Run(ctx context.Context) (retention.RunResult, error)
}

// RetentionHandler serves the data-retention policy and the sweep behind it.
type RetentionHandler struct {
	svc RetentionService
}

// NewRetentionHandler returns a handler backed by svc.
func NewRetentionHandler(svc RetentionService) *RetentionHandler {
	return &RetentionHandler{svc: svc}
}

// Register wires the routes onto mux.
//
// These are the only routes in this service that are not reads. They live here
// rather than on the orchestrator because this service owns the three tables a
// policy governs, which is the same reason the platform queries logs and traces
// here directly instead of hopping through it.
func (h *RetentionHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/retention", h.get)
	mux.HandleFunc("PUT /settings/retention", h.update)
	mux.HandleFunc("POST /retention/run", h.run)
}

// retentionPolicyResponse is the wire shape of the policy. Zero on an axis means
// keep forever, which is the default and what this service did before the policy
// existed.
type retentionPolicyResponse struct {
	LogsDays   int        `json:"logs_days"`
	TracesDays int        `json:"traces_days"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

// retentionUpdateRequest is the body of a policy save.
//
// Pointers because zero is a meaningful value here, and the one it means is
// "keep this stream forever". Plain ints would decode an omitted field to zero
// and be indistinguishable from someone asking for exactly that — so a request
// naming only logs_days would quietly switch trace retention off, which is the
// direction that loses data rather than the direction that keeps it. Both fields
// are required, and update rejects a body missing either.
type retentionUpdateRequest struct {
	LogsDays   *int `json:"logs_days"`
	TracesDays *int `json:"traces_days"`
}

// retentionRunResponse reports what a sweep deleted. A cutoff is null for an axis
// set to keep everything, which is what distinguishes a zero count that means
// "nothing was asked for" from one that means "nothing had expired".
type retentionRunResponse struct {
	LogsDeleted           int64      `json:"logs_deleted"`
	TracesDeleted         int64      `json:"traces_deleted"`
	TraceSummariesDeleted int64      `json:"trace_summaries_deleted"`
	LogsCutoff            *time.Time `json:"logs_cutoff"`
	TracesCutoff          *time.Time `json:"traces_cutoff"`
	DurationMs            int64      `json:"duration_ms"`
}

func toPolicyResponse(p retention.Policy) retentionPolicyResponse {
	return retentionPolicyResponse{
		LogsDays:   p.LogsDays,
		TracesDays: p.TracesDays,
		UpdatedAt:  p.UpdatedAt,
	}
}

// get returns the stored policy.
//
//	@Summary		Get the data-retention policy
//	@Description	How many days of log events and of traces this installation keeps. Zero on
//	@Description	an axis means keep forever, which is the default — an installation that has
//	@Description	never configured retention reads back as zeros rather than as an error.
//	@Tags			retention
//	@Produce		json
//	@Success		200	{object}	retentionPolicyResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/settings/retention [get]
func (h *RetentionHandler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), settingsTimeout)
	defer cancel()

	policy, err := h.svc.Get(ctx)
	if err != nil {
		slog.Error("api: get retention policy", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to read the retention policy")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPolicyResponse(policy))
}

// update stores the policy.
//
//	@Summary		Save the data-retention policy
//	@Description	Both windows are supplied on every save. Zero keeps that stream forever;
//	@Description	the maximum is 3650 days. Saving does not delete anything — the sweep runs
//	@Description	nightly, or on demand through POST /retention/run.
//	@Tags			retention
//	@Accept			json
//	@Produce		json
//	@Param			body	body		retentionUpdateRequest	true	"The two retention windows, in days. Both are required"
//	@Success		200		{object}	retentionPolicyResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"a window is missing, negative, or beyond 3650 days"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/settings/retention [put]
func (h *RetentionHandler) update(w http.ResponseWriter, r *http.Request) {
	var req retentionUpdateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// A save replaces the policy outright, so a body that names one window is not
	// a partial update — it is a request to set the other one to zero, which means
	// keep that stream forever. Refusing is the only reading that cannot silently
	// switch retention off for the stream nobody mentioned.
	if req.LogsDays == nil || req.TracesDays == nil {
		httpx.WriteError(w, http.StatusBadRequest,
			"logs_days and traces_days are both required (0 keeps everything)")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), settingsTimeout)
	defer cancel()

	policy, err := h.svc.Update(ctx, retention.Update{
		LogsDays:   *req.LogsDays,
		TracesDays: *req.TracesDays,
	})
	if err != nil {
		if errors.Is(err, retention.ErrInvalidDays) {
			httpx.WriteError(w, http.StatusBadRequest,
				"retention must be between 0 and 3650 days (0 keeps everything)")
			return
		}
		slog.Error("api: save retention policy", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "failed to save the retention policy")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toPolicyResponse(policy))
}

// run enforces the stored policy now.
//
//	@Summary		Enforce the retention policy now
//	@Description	Deletes what the stored policy no longer keeps and reports how much went.
//	@Description	This is what the chart's nightly job calls. A policy that keeps everything
//	@Description	is a 200 with zero counts and null cutoffs rather than an error — being
//	@Description	asked to do nothing is an answer.
//	@Description
//	@Description	A trace is deleted whole: its records go with its summary, so a swept trace
//	@Description	never becomes one that lists but cannot be opened. Stored model prices are
//	@Description	never swept, because a traced call's cost points into that history.
//	@Tags			retention
//	@Produce		json
//	@Success		200	{object}	retentionRunResponse
//	@Failure		409	{object}	httpx.ErrorResponse	"another sweep is already running"
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/retention/run [post]
func (h *RetentionHandler) run(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), sweepTimeout)
	defer cancel()

	result, err := h.svc.Run(ctx)
	if err != nil {
		if errors.Is(err, retention.ErrRunInProgress) {
			httpx.WriteError(w, http.StatusConflict, "a retention sweep is already running")
			return
		}
		slog.Error("api: run retention sweep", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, "the retention sweep failed")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, retentionRunResponse{
		LogsDeleted:           result.LogsDeleted,
		TracesDeleted:         result.TracesDeleted,
		TraceSummariesDeleted: result.TraceSummariesDeleted,
		LogsCutoff:            result.LogsCutoff,
		TracesCutoff:          result.TracesCutoff,
		DurationMs:            result.Duration.Milliseconds(),
	})
}
