package health

import (
	"context"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// requestTimeout bounds the whole sweep. Comfortably above four probes at their
// own three-second ceiling, so the handler never gives up before the probes do —
// a page that timed out would report nothing at all, which is the least useful
// answer available.
const requestTimeout = 20 * time.Second

// Handler serves the dependency report.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the route to mux. It sits under /settings because it is an
// admin read about this installation, alongside the agent and retention routes,
// and deliberately not under /healthz — that one is the liveness probe and
// answers about this process, not about what it depends on.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/health", h.get)
}

// dependencyResponse is one row of the report.
type dependencyResponse struct {
	Name string `json:"name"`
	// Configured false means this installation does not have the dependency,
	// which is not the same as it being down.
	Configured bool   `json:"configured"`
	Reachable  bool   `json:"reachable"`
	Detail     string `json:"detail,omitempty"`
	LatencyMs  int64  `json:"latencyMs,omitempty"`
}

type healthResponse struct {
	Dependencies []dependencyResponse `json:"dependencies"`
}

// get godoc
//
//	@Summary		Whether this orchestrator can reach what it depends on
//	@Description	One round trip to each of Postgres, Redis, NATS and Kubernetes. It
//	@Description	is deliberately shallow: a reachable dependency is one that answered,
//	@Description	not one that is healthy in any deeper sense. A dependency this
//	@Description	installation never configured reports `configured: false` rather than
//	@Description	as a failure — running without cluster access is supported.
//	@Tags			settings
//	@Produce		json
//	@Success		200	{object}	healthResponse
//	@Router			/settings/health [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	deps := h.svc.Check(ctx)
	out := healthResponse{Dependencies: make([]dependencyResponse, 0, len(deps))}
	for _, d := range deps {
		// A conversion rather than a field-by-field copy, which is legal only while
		// the two shapes coincide — Go ignores the tags, not the fields. The wire
		// type still exists in its own right: it is what carries the JSON names and
		// the omitempty rules, and the day the report needs a field the page does
		// not, this line stops compiling and says so.
		out.Dependencies = append(out.Dependencies, dependencyResponse(d))
	}
	// Always 200. The report is the answer even when every row in it is a failure —
	// a non-2xx here would make the page that renders it indistinguishable from the
	// orchestrator itself being down, which is the one thing it can already tell you.
	httpx.WriteJSON(w, http.StatusOK, out)
}
