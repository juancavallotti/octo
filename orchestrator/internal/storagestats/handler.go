package storagestats

import (
	"context"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// requestTimeout bounds the whole report, comfortably above two probes at their own
// ceiling so the handler never gives up before they do.
const requestTimeout = 15 * time.Second

// Handler serves the storage report.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the route to mux. It sits under /settings alongside
// /settings/health for the same reason that one does: it is an admin read about
// this installation rather than about any one deployment's data.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/storage", h.get)
}

// get godoc
//
//	@Summary		How full the platform's two stores are
//	@Description	Memory against the ceiling, hit rate and evictions for Redis; connection
//	@Description	pool usage and the size of the KV table for Postgres. Either half is null
//	@Description	when this installation does not have that store or cannot reach it, with
//	@Description	the reason alongside — an installation with no Redis is supported, and is
//	@Description	not the same as one whose Redis is down.
//	@Tags			settings
//	@Produce		json
//	@Success		200	{object}	Stats
//	@Router			/settings/storage [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	// Always 200, for the same reason the health page is: the report is the answer
	// even when both halves are absent. A non-2xx would make the page that renders
	// it indistinguishable from the orchestrator itself being down.
	httpx.WriteJSON(w, http.StatusOK, h.svc.Collect(ctx))
}
