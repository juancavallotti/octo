// Package api is the dev sidecar's HTTP surface: the commands the orchestrator
// sends it and the diagnostics anyone debugging a dev run wants to read.
//
// Everything except the probes requires a bearer token, and the probes are
// exempt because a kubelet probe cannot carry one. That split is the whole
// authorisation model, and it is enough because of where this listens: a port on
// a pod whose only routed peer is the orchestrator. The token is what stops
// anything else inside the cluster from driving someone's dev run.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/juancavallotti/octo/sidecars/dev/internal/reload"
	"github.com/juancavallotti/octo/sidecars/dev/internal/runtimeprobe"
)

const (
	// reloadTimeout bounds a reload request. Longer than the orchestrator client's
	// own timeout so a pull that is merely slow gets to finish rather than being cut
	// off at the last hop and reported as a failure it is not.
	reloadTimeout = 60 * time.Second
	// readTimeout bounds the read-only endpoints.
	readTimeout = 10 * time.Second
)

// reloader is the pull-and-apply cycle. Declared here, in the consumer; satisfied
// by *reload.Coordinator.
type reloader interface {
	Reload(ctx context.Context) (reload.Outcome, error)
	State() reload.State
}

// workspaceFS is the managed directory. Satisfied by *workspace.Workspace.
type workspaceFS interface {
	Dir() string
	List() ([]string, error)
	RemoveConfig() error
}

// prober reads the runtime beside us. Satisfied by *runtimeprobe.Prober.
type prober interface {
	Addr() string
	Status(ctx context.Context) runtimeprobe.Status
	Metrics(ctx context.Context) ([]byte, string, error)
}

// Handler serves the sidecar's endpoints.
type Handler struct {
	reload reloader
	ws     workspaceFS
	probe  prober
	token  string
}

// NewHandler returns a Handler. token authenticates every route but the probes;
// an empty token is rejected at startup, not here, so this type has no
// "unauthenticated mode" to accidentally fall into.
func NewHandler(r reloader, ws workspaceFS, p prober, token string) *Handler {
	return &Handler{reload: r, ws: ws, probe: p, token: token}
}

// Register attaches the sidecar routes to mux.
//
// The probes come first and unauthenticated; everything after them is wrapped in
// the bearer check. Registering them through one function keeps that split
// visible in one place rather than spread across handler bodies.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", h.healthz)
	mux.HandleFunc("GET /readyz", h.readyz)

	mux.Handle("POST /reload", h.authed(h.doReload))
	mux.Handle("GET /status", h.authed(h.status))
	mux.Handle("GET /diagnostics", h.authed(h.diagnostics))
	mux.Handle("GET /metrics", h.authed(h.metrics))
	mux.Handle("DELETE /workspace/config", h.authed(h.removeConfig))
}

// Endpoints lists what Register mounted, for the startup log. There is no OpenAPI
// document in this repo, so the log line is the API's documentation.
func Endpoints() []string {
	return []string{
		"GET /healthz", "GET /readyz", "POST /reload", "GET /status",
		"GET /diagnostics", "GET /metrics", "DELETE /workspace/config",
	}
}

// healthz answers liveness, unconditionally.
//
// Unconditional for the same reason the runtime's own /healthz is
// (runtime/services/observability/probes.go:37): liveness asks whether the process
// is wedged, and this handler running at all is the answer. Gating it on the
// orchestrator being reachable would restart a perfectly healthy sidecar every
// time the orchestrator rolled.
func (h *Handler) healthz(w http.ResponseWriter, _ *http.Request) {
	writePlain(w, http.StatusOK, "ok")
}

// readyz answers readiness: 200 once the workspace has been populated at least
// once, 503 before that.
//
// This is what orders pod startup. The sidecar runs as a native sidecar — an init
// container with restartPolicy: Always — so the runtime container does not start
// until this reports ready, which means `octo` finds a populated directory on its
// first look rather than racing the first pull.
func (h *Handler) readyz(w http.ResponseWriter, _ *http.Request) {
	st := h.reload.State()
	switch {
	case st.Expired:
		writePlain(w, http.StatusServiceUnavailable, "expired")
	case st.Pulls > 0:
		writePlain(w, http.StatusOK, "ready")
	case st.LastError != "":
		writePlain(w, http.StatusServiceUnavailable, "waiting for first pull: "+st.LastError)
	default:
		writePlain(w, http.StatusServiceUnavailable, "waiting for first pull")
	}
}

// doReload is the hot-reload command: pull the bundle, stage it, rewrite the
// config. It takes no request body — the trigger carries no payload, because the
// orchestrator it would come from is the same place the sidecar pulls from.
func (h *Handler) doReload(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), reloadTimeout)
	defer cancel()

	outcome, err := h.reload.Reload(ctx)
	if err != nil {
		// 503, not 500: every reason a reload fails is a dependency being unavailable
		// (the orchestrator unreachable, the dev run gone, the volume unwritable), and
		// the orchestrator's notifier treats this as best-effort either way.
		slog.Warn("reload failed", "error", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if outcome == reload.OutcomeCoalesced {
		// Accepted, not OK: a pull was already in flight and will repeat, so the work
		// is guaranteed but has not happened yet. Saying 200 here would claim the
		// workspace is current when it may not be for another second.
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "coalesced"})
		return
	}
	writeJSON(w, http.StatusOK, h.reload.State())
}

// status reports the reload state and the runtime's health together, because the
// question behind it is always "is the app I saved the app that is running", and
// answering that needs both halves.
func (h *Handler) status(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()

	writeJSON(w, http.StatusOK, statusResponse{
		Workspace: h.ws.Dir(),
		Reload:    h.reload.State(),
		Runtime:   h.probe.Status(ctx),
	})
}

// statusResponse is the wire shape of GET /status.
type statusResponse struct {
	Workspace string              `json:"workspace"`
	Reload    reload.State        `json:"reload"`
	Runtime   runtimeprobe.Status `json:"runtime"`
}

// diagnostics is status plus what is actually on disk. Separate from /status
// because listing the workspace is the expensive, occasionally-wanted half: the
// first question when a reload did not do what someone expected is "what does the
// runtime actually see".
func (h *Handler) diagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()

	files, err := h.ws.List()
	out := diagnosticsResponse{
		statusResponse: statusResponse{
			Workspace: h.ws.Dir(),
			Reload:    h.reload.State(),
			Runtime:   h.probe.Status(ctx),
		},
		RuntimeAdmin: h.probe.Addr(),
		Files:        files,
	}
	if err != nil {
		// A listing failure is reported inline rather than as a 500: the rest of the
		// diagnostics is still the information someone asked for, and losing all of it
		// because one part failed is the opposite of useful when debugging.
		out.FilesError = err.Error()
	}
	writeJSON(w, http.StatusOK, out)
}

// diagnosticsResponse is the wire shape of GET /diagnostics.
type diagnosticsResponse struct {
	statusResponse
	RuntimeAdmin string   `json:"runtimeAdmin"`
	Files        []string `json:"files"`
	FilesError   string   `json:"filesError,omitempty"`
}

// metrics passes the runtime's Prometheus exposition through unchanged, so a dev
// run's metrics are readable without exposing the runtime's admin port.
func (h *Handler) metrics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readTimeout)
	defer cancel()

	body, contentType, err := h.probe.Metrics(ctx)
	if err != nil {
		// The ordinary cause is a runtime started without metrics enabled, which is a
		// statement about the runtime rather than a failure of this endpoint.
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// removeConfig deletes the config, so the runtime reloads to an empty generation.
// A stop the runtime observes without the pod being killed — useful for pausing a
// dev run's sources while keeping the pod (and its logs, and its host) around.
func (h *Handler) removeConfig(w http.ResponseWriter, _ *http.Request) {
	if err := h.ws.RemoveConfig(); err != nil {
		slog.Error("remove config", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// authed wraps a handler in the bearer-token check.
func (h *Handler) authed(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !h.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	})
}

// authorized reports whether the request carries the sidecar token.
//
// Compared in constant time: a token compared with == leaks its prefix through
// timing, and this one guards the ability to rewrite what a user's app is running.
func (h *Handler) authorized(r *http.Request) bool {
	const prefix = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return false
	}
	presented := strings.TrimSpace(header[len(prefix):])
	return subtle.ConstantTimeCompare([]byte(presented), []byte(h.token)) == 1
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// Status and headers are already out; logging is all that is left.
		slog.Error("api: encode response", "error", err)
	}
}

// writePlain writes a text/plain response, matching the runtime's probe output so
// the two ports read alike under curl.
func writePlain(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	// A cached probe answer is worse than no answer: the whole value is that it
	// reflects the state right now.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body + "\n"))
}
