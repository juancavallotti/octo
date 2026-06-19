package folder

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/eip-go/orchestrator/internal/http"
)

// requestTimeout bounds the database work behind a single request.
const requestTimeout = 5 * time.Second

// Handler serves the folder REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the folder routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /folders", h.create)
	mux.HandleFunc("GET /folders", h.list)
	mux.HandleFunc("GET /folders/{id}", h.get)
	mux.HandleFunc("PUT /folders/{id}", h.update)
	mux.HandleFunc("DELETE /folders/{id}", h.delete)
	mux.HandleFunc("GET /folders/{id}/integrations", h.listIntegrations)
	mux.HandleFunc("PUT /folders/{id}/integrations/{integrationId}", h.addIntegration)
	mux.HandleFunc("DELETE /folders/{id}/integrations/{integrationId}", h.removeIntegration)
}

// folderRequest is the create/update payload. ParentID is null for a root.
type folderRequest struct {
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
}

// folderResponse is the wire representation of a folder. Children is present
// only in the tree listing; single-folder reads omit it.
type folderResponse struct {
	ID       string           `json:"id"`
	ParentID *string          `json:"parentId"`
	Name     string           `json:"name"`
	Children []folderResponse `json:"children,omitempty"`
}

// integrationResponse is the wire representation of an integration in a folder's
// membership listing.
type integrationResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Definition  string    `json:"definition"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// toResponse maps a folder (and any nested children) to its wire form.
func toResponse(f Folder) folderResponse {
	out := folderResponse{ID: f.ID, ParentID: f.ParentID, Name: f.Name}
	if len(f.Children) > 0 {
		out.Children = make([]folderResponse, 0, len(f.Children))
		for _, c := range f.Children {
			out.Children = append(out.Children, toResponse(c))
		}
	}
	return out
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req folderRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	f, err := h.svc.Create(ctx, req.Name, req.ParentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(f))
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	tree, err := h.svc.Tree(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}

	out := make([]folderResponse, 0, len(tree))
	for _, f := range tree {
		out = append(out, toResponse(f))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	f, err := h.svc.Get(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(f))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req folderRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	f, err := h.svc.Update(ctx, r.PathValue("id"), req.Name, req.ParentID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(f))
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

func (h *Handler) listIntegrations(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.ListIntegrations(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}

	out := make([]integrationResponse, 0, len(items))
	for _, it := range items {
		out = append(out, integrationResponse(it))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) addIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.AddIntegration(ctx, r.PathValue("id"), r.PathValue("integrationId")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) removeIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.RemoveIntegration(ctx, r.PathValue("id"), r.PathValue("integrationId")); err != nil {
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
		httpx.WriteError(w, http.StatusNotFound, "not found")
	default:
		slog.Error("folder handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
