package snapshot

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// requestTimeout bounds the database work behind a single request.
const requestTimeout = 5 * time.Second

// Handler serves the snapshot REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the snapshot routes to mux. Create/list are nested under an
// integration; delete addresses a snapshot directly.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations/{id}/snapshots", h.create)
	mux.HandleFunc("GET /integrations/{id}/snapshots", h.listByIntegration)
	mux.HandleFunc("DELETE /snapshots/{id}", h.delete)
}

// createRequest is the create payload: the tag to freeze the current definition
// under.
type createRequest struct {
	Tag string `json:"tag"`
}

// snapshotResponse is the wire representation of a snapshot. The frozen definition
// is deliberately omitted — clients list and pick tags; the deploy resolves the
// definition server-side from the chosen snapshot.
type snapshotResponse struct {
	ID            string    `json:"id"`
	IntegrationID string    `json:"integrationId"`
	Tag           string    `json:"tag"`
	CreatedAt     time.Time `json:"createdAt"`
}

func toResponse(s Snapshot) snapshotResponse {
	return snapshotResponse{
		ID:            s.ID,
		IntegrationID: s.IntegrationID,
		Tag:           s.Tag,
		CreatedAt:     s.CreatedAt,
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	s, err := h.svc.Create(ctx, r.PathValue("id"), req.Tag)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(s))
}

func (h *Handler) listByIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.ListByIntegration(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]snapshotResponse, 0, len(items))
	for _, s := range items {
		out = append(out, toResponse(s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are
// logged and reported generically so internals do not leak to clients.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrTagExists):
		httpx.WriteError(w, http.StatusConflict, "a snapshot with this tag already exists")
	case errors.Is(err, ErrIntegrationNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "snapshot not found")
	case errors.Is(err, ErrSnapshotInUse):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("snapshot handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
