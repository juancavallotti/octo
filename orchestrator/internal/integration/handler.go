package integration

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

// Handler serves the integration REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the integration routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations", h.create)
	mux.HandleFunc("GET /integrations", h.list)
	mux.HandleFunc("GET /integrations/{id}", h.get)
	mux.HandleFunc("PUT /integrations/{id}", h.update)
	mux.HandleFunc("DELETE /integrations/{id}", h.delete)
}

// integrationRequest is the create/update payload.
type integrationRequest struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

// integrationResponse is the wire representation of an integration.
type integrationResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Definition  string    `json:"definition"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// toResponse maps the domain model to its wire form. The field layouts match,
// so a direct conversion suffices; if they diverge this stops compiling, which
// is the signal to write an explicit mapping.
func toResponse(it Integration) integrationResponse {
	return integrationResponse(it)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req integrationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	it, err := h.svc.Create(ctx, req.Name, req.Definition)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(it))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.List(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}

	out := make([]integrationResponse, 0, len(items))
	for _, it := range items {
		out = append(out, toResponse(it))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	it, err := h.svc.Get(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(it))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req integrationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	it, err := h.svc.Update(ctx, r.PathValue("id"), req.Name, req.Definition)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(it))
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
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	default:
		slog.Error("integration handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
