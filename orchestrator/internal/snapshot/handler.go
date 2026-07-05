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
// integration; delete addresses a snapshot directly. The resource routes serve
// the resources frozen alongside the definition: a deployed runtime reads them
// from the content route (kind and name are query params, not path segments,
// because a name is path-like and may contain '/').
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations/{id}/snapshots", h.create)
	mux.HandleFunc("GET /integrations/{id}/snapshots", h.listByIntegration)
	mux.HandleFunc("DELETE /snapshots/{id}", h.delete)
	mux.HandleFunc("GET /snapshots/{id}/resources", h.listResources)
	mux.HandleFunc("GET /snapshots/{id}/resources/content", h.resourceContent)
}

// createRequest is the create payload: the tag to freeze the current definition
// under.
type createRequest struct {
	Tag string `json:"tag"`
}

// snapshotResponse is the wire representation of a snapshot, including its frozen
// definition so clients can show version-scoped stats (e.g. the detail pane's
// Definition panel) without a second fetch. ListByIntegration already loads it.
type snapshotResponse struct {
	ID            string    `json:"id"`
	IntegrationID string    `json:"integrationId"`
	Tag           string    `json:"tag"`
	Definition    string    `json:"definition"`
	CreatedAt     time.Time `json:"createdAt"`
}

func toResponse(s Snapshot) snapshotResponse {
	return snapshotResponse{
		ID:            s.ID,
		IntegrationID: s.IntegrationID,
		Tag:           s.Tag,
		Definition:    s.Definition,
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

// resourceResponse is the wire representation of a frozen resource. Content is
// omitted from the listing (it may be large); the content route serves the raw
// bytes.
type resourceResponse struct {
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

func (h *Handler) listResources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.ListResources(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]resourceResponse, 0, len(items))
	for _, res := range items {
		out = append(out, resourceResponse{Kind: res.Kind, Name: res.Name, CreatedAt: res.CreatedAt})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// resourceContent serves one frozen resource's raw bytes. kind and name are
// required query params; a missing resource is a 404. This is the endpoint the
// runtime's k8s resource loader calls.
func (h *Handler) resourceContent(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	name := r.URL.Query().Get("name")
	if kind == "" || name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "kind and name query params are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	content, found, err := h.svc.ResourceContent(ctx, r.PathValue("id"), kind, name)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "resource not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := w.Write(content); err != nil {
		slog.Error("snapshot handler: write resource content", "error", err)
	}
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
