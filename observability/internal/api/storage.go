package api

import (
	"context"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/observability/internal/httpx"
	"github.com/juancavallotti/octo/observability/internal/storagestats"
)

// storageTimeout bounds the whole report, comfortably above two probes at their
// own ceiling so the handler never gives up before they do.
const storageTimeout = 15 * time.Second

// StorageCollector is what the handler needs from the storagestats package. The
// handler depends on the interface so it can be tested without either store.
type StorageCollector interface {
	Collect(ctx context.Context) storagestats.Stats
}

// StorageHandler serves the storage report.
type StorageHandler struct {
	svc StorageCollector
}

// NewStorageHandler returns a handler backed by svc.
func NewStorageHandler(svc StorageCollector) *StorageHandler {
	return &StorageHandler{svc: svc}
}

// Register wires the route onto mux. It sits under /settings beside the retention
// policy because it is an admin read about this installation rather than about
// any one deployment's data.
func (h *StorageHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/storage", h.get)
}

// get godoc
//
//	@Summary		How full the platform's two stores are
//	@Description	Memory against the ceiling, hit rate and evictions for Redis; this
//	@Description	service's connection pool usage and the size of the object store's
//	@Description	table for Postgres. Either half is null when this installation does
//	@Description	not have that store or cannot reach it, with the reason alongside — an
//	@Description	installation with no Redis is supported, and is not the same as one
//	@Description	whose Redis is down.
//	@Tags			storage
//	@Produce		json
//	@Success		200	{object}	storagestats.Stats
//	@Router			/settings/storage [get]
func (h *StorageHandler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), storageTimeout)
	defer cancel()

	// Always 200: the report is the answer even when both halves are absent. A
	// non-2xx would make the page that renders it indistinguishable from this
	// service itself being down.
	httpx.WriteJSON(w, http.StatusOK, h.svc.Collect(ctx))
}
