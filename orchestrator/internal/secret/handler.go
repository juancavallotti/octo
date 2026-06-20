package secret

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/eip-go/orchestrator/internal/http"
)

// requestTimeout bounds the database + Kubernetes work behind a single request.
const requestTimeout = 15 * time.Second

// Handler serves the cluster-secret REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the secret routes to mux. Secrets are addressed by name; the
// value is write-only (set via PUT, never read back).
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /secrets", h.list)
	mux.HandleFunc("PUT /secrets/{name}", h.set)
	mux.HandleFunc("DELETE /secrets/{name}", h.delete)
}

// secretResponse is the wire representation of a catalog entry. It deliberately
// has no value field — values are never returned.
type secretResponse struct {
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// toResponse maps the domain model to its wire form. The field layouts match, so
// a direct conversion suffices; if they diverge this stops compiling, which is the
// signal to write an explicit mapping.
func toResponse(s Secret) secretResponse {
	return secretResponse(s)
}

// setRequest is the body of a set request: the secret value to store.
type setRequest struct {
	Value string `json:"value"`
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.List(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]secretResponse, 0, len(items))
	for _, s := range items {
		out = append(out, toResponse(s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	var req setRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	s, err := h.svc.Create(ctx, r.PathValue("name"), req.Value)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(s))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	force := r.URL.Query().Get("force") == "true"
	if err := h.svc.Delete(ctx, r.PathValue("name"), force); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are logged
// and reported generically so internals do not leak to clients.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "invalid secret name (use UPPER_SNAKE_CASE)")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "secret not found")
	case errors.Is(err, ErrInUse):
		httpx.WriteError(w, http.StatusConflict, "secret is in use by a deployment")
	case errors.Is(err, ErrUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, "secrets are not available")
	default:
		slog.Error("secret handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
