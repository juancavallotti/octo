package devrun

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

const (
	// requestTimeout bounds the cluster work behind one dev-run request. Reads come
	// from the informer cache and cost nothing, but a Run creates three objects and
	// waits on the API server, so this matches the deployment handler's bound rather
	// than the database-only handlers' shorter one.
	requestTimeout = 15 * time.Second
	// defaultLogTail bounds the history replayed when a log stream connects, so a dev
	// run that has been up for an hour does not dump the whole hour before tailing
	// begins.
	defaultLogTail = 500
)

// Handler serves the dev-run REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the dev-run routes to mux.
//
// The routes fall into two groups that differ in *who authorises them*, and they are
// addressed differently for exactly that reason:
//
//   - A user's routes carry ?userId=. The caller is the platform BFF, which holds the
//     session and states whose behalf it is acting on — the same arrangement as
//     /users/{userId}/apikeys — and every one of these operations is confined to that
//     user's own runs. The user is a scope rather than part of the address, because a
//     dev run is addressed by its derived id.
//   - The sidecar's two routes carry a bearer token instead. A pod is not a user, its
//     token authorises exactly one run, and these two paths are frozen by the
//     sidecar's own client (sidecars/dev/internal/bundle) — so they are flat, and
//     nesting them under a user would be both wrong and a wire break.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /devruns", h.ensure)
	mux.HandleFunc("GET /devruns", h.list)
	mux.HandleFunc("GET /devruns/{id}", h.status)
	mux.HandleFunc("DELETE /devruns/{id}", h.stop)
	mux.HandleFunc("POST /devruns/{id}/reload", h.reload)
	mux.HandleFunc("GET /devruns/{id}/logs", h.logs)

	mux.HandleFunc("GET /devruns/{id}/bundle", h.bundle)
	mux.HandleFunc("POST /devruns/{id}/expire", h.expire)
}

// ensureRequest is the body of a Run: whose run, and which integration.
//
// The user id is stated by the caller rather than inferred, because the orchestrator
// has no session — the platform BFF holds it, having already authenticated the
// request.
type ensureRequest struct {
	UserID        string `json:"userId"`
	IntegrationID string `json:"integrationId"`
}

// devRunResponse is the wire representation of a dev run: the cluster's account of it,
// its stable public address, and the sidecar's own view when one was available.
//
// There is no "port" and no "namespace" here, unlike the local runner's status: a dev
// pod owns its network namespace, so the listen port is a platform constant and the
// address is the derived host.
type devRunResponse struct {
	ID            string `json:"id"`
	UserID        string `json:"userId"`
	IntegrationID string `json:"integrationId"`
	// Status is pending|running|failed, as computed from the workload and its pods.
	Status string `json:"status"`
	Ready  bool   `json:"ready"`
	Reason string `json:"reason,omitempty"`
	// Restarts is the total across the run's pods, which is how a crash loop shows up
	// in a UI that otherwise only reports the current phase.
	Restarts int32 `json:"restarts,omitempty"`
	// Host and TestURL are absent for an integration that serves no HTTP.
	Host    string `json:"host,omitempty"`
	TestURL string `json:"testUrl,omitempty"`
	// CreatedAt and LastActivity are pointers so an unknown one is omitted rather than
	// reported as the zero time, which a client would render as 1 January year 1.
	CreatedAt    *time.Time `json:"createdAt,omitempty"`
	LastActivity *time.Time `json:"lastActivity,omitempty"`
	// Sidecar is the sidecar's own status document, passed through as it produced it.
	// Absent when the sidecar was not asked or did not answer in time.
	Sidecar json.RawMessage `json:"sidecar,omitempty"`
}

// ensureResponse adds what Ensure alone can report: whether this call started the run
// or attached to one that was already there. Surfaced rather than hidden, because two
// tabs on one integration deliberately share a pod and a user who clicks Run should be
// told when they landed on a session rather than starting one.
type ensureResponse struct {
	devRunResponse
	Created bool `json:"created"`
}

func toResponse(run DevRun) devRunResponse {
	resp := devRunResponse{
		ID:            run.ID,
		UserID:        run.UserID,
		IntegrationID: run.IntegrationID,
		Status:        run.Detail.Phase,
		Ready:         run.Detail.ReadyReplicas > 0,
		Reason:        run.Detail.Reason,
		Host:          run.Host,
		TestURL:       run.TestURL,
		Sidecar:       run.Sidecar,
	}
	// A run created moments ago has no phase, because the informer that would compute
	// one has not seen the workload yet. Passing "" through would make the editor
	// render an unknown badge for the first second of every Run; pending is both
	// friendlier and true.
	if resp.Status == "" {
		resp.Status = kube.StatusPending
	}
	for _, p := range run.Detail.Pods {
		resp.Restarts += p.Restarts
	}
	if !run.CreatedAt.IsZero() {
		t := run.CreatedAt
		resp.CreatedAt = &t
	}
	if !run.LastActivity.IsZero() {
		t := run.LastActivity
		resp.LastActivity = &t
	}
	return resp
}

// ensure starts a dev run, or attaches to the one already running the integration.
// 201 for a create and 200 for an attach, so the outcome is visible in the status line
// as well as in the body.
func (h *Handler) ensure(w http.ResponseWriter, r *http.Request) {
	var req ensureRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	result, err := h.svc.Ensure(ctx, req.UserID, req.IntegrationID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	status := http.StatusOK
	if result.Created {
		status = http.StatusCreated
	}
	httpx.WriteJSON(w, status, ensureResponse{
		devRunResponse: toResponse(result.DevRun),
		Created:        result.Created,
	})
}

// list answers "what am I running?", and with ?integrationId= the editor's
// Run-vs-Attach question. Both are one label lookup against the informer cache.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	runs, err := h.svc.ListByUser(ctx, r.URL.Query().Get("userId"), r.URL.Query().Get("integrationId"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]devRunResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, toResponse(run))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// status reports one dev run: the cluster's account of it, with the sidecar's own view
// folded in when the sidecar answered promptly.
//
// One route rather than the two the design sketched — cluster status here, sidecar
// status beside it — because the sidecar half is what distinguishes "my save reached the
// pod" from "my save is still in Postgres", and a caller that has to make two requests
// to learn that will make the one that cannot answer it. The sidecar is asked with a
// short timeout and simply left out when it does not reply, which is the normal state
// while a pod is still starting.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	run, err := h.svc.Status(ctx, r.URL.Query().Get("userId"), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(run))
}

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Stop(ctx, r.URL.Query().Get("userId"), r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reload is the explicit "pick up what I just saved". The per-edit trigger is not
// here: it is on the write path (integration and resource writes notify the service),
// so this route exists for a user asking directly.
//
// It answers 204 rather than a status snapshot, and that is honest rather than lazy: a
// reload's effect is the sidecar pulling and the runtime re-reading its directory, so
// any status read taken at this moment would describe the state before the reload. The
// caller polls status for the state after it.
func (h *Handler) reload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Reload(ctx, r.URL.Query().Get("userId"), r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// logs streams the dev run's runtime logs as plain text. With ?follow=1 it tails until
// the client disconnects, which cancels the request context and closes the Kubernetes
// stream.
//
// It deliberately does not take requestTimeout: a follow stream is long-lived by
// design, and the tail bound keeps the initial replay small instead.
func (h *Handler) logs(w http.ResponseWriter, r *http.Request) {
	follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"
	tail := int64(defaultLogTail)
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			tail = n
		}
	}

	stream, err := h.svc.PodLogs(r.Context(), r.URL.Query().Get("userId"), r.PathValue("id"), follow, tail)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer func() { _ = stream.Close() }()

	httpx.StreamText(w, r, stream, "dev run log stream")
}

// bundle serves the dev run's workspace: the integration's saved definition and its
// resources. Called by the run's own sidecar, authorised by the dev-run token.
//
// The 404 this can return is load-bearing on the other side of the wire: the sidecar
// treats it as terminal — this run no longer addresses anything real — and asks to be
// expired, where it retries a 5xx forever. So a run that is genuinely gone must reach
// here as ErrNotFound and not as an internal error.
func (h *Handler) bundle(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	b, err := h.svc.Bundle(ctx, r.PathValue("id"), bearer(r))
	if err != nil {
		h.writeError(w, err)
		return
	}
	// The Bundle type's json tags are the wire contract with the sidecar, so it is
	// written directly rather than mapped through a second copy of the same shape.
	httpx.WriteJSON(w, http.StatusOK, b)
}

// expire tears the dev run down at its own sidecar's request, which is what a sidecar
// does when its bundle pull 404s. Separate from DELETE /devruns/{id} because that route
// authorises a user action and a pod holding a dev-run token is not a user.
func (h *Handler) expire(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Expire(ctx, r.PathValue("id"), bearer(r)); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bearer returns the token from the Authorization header, or "" when there is none.
//
// The value is compared in constant time by the service against the hash recorded on
// the workload; this only parses. The prefix check is case-insensitive because RFC 7235
// says the scheme is, and a sidecar that sent "bearer" would otherwise be rejected for
// a reason nobody would find.
func bearer(r *http.Request) string {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are logged and
// reported generically so internals do not leak to clients.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, "dev runs are not available")
	case errors.Is(err, ErrUserRequired):
		httpx.WriteError(w, http.StatusBadRequest, "userId is required")
	case errors.Is(err, ErrIntegrationNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "dev run not found")
	case errors.Is(err, ErrUnauthorized):
		httpx.WriteError(w, http.StatusUnauthorized, "dev run token rejected")
	case errors.Is(err, ErrHostTaken):
		// Logged rather than returned verbatim: the message names the dev run holding
		// the host, and that run belongs to somebody else. The operator can find it in
		// this log line; the caller only needs to know their Run was refused.
		slog.Error("devrun handler", "error", err)
		httpx.WriteError(w, http.StatusConflict, "this dev run's public host is already in use")
	case errors.Is(err, ErrNotStarted):
		httpx.WriteError(w, http.StatusServiceUnavailable, "the dev run has no pod to read yet")
	case errors.Is(err, ErrSidecarUnreachable):
		httpx.WriteError(w, http.StatusBadGateway, "the dev run's sidecar is not answering")
	default:
		slog.Error("devrun handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
