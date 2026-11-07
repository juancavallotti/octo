package environment

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/authz"
	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// requestTimeout bounds the database + Kubernetes work behind a single request.
// Generous, because a create provisions several objects in a new namespace.
const requestTimeout = 30 * time.Second

// Handler serves the environment REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the environment routes to mux. Environments are addressed by
// name.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /environments", h.list)
	mux.HandleFunc("POST /environments", h.create)
	mux.HandleFunc("GET /environments/{name}", h.get)
	mux.HandleFunc("PATCH /environments/{name}", h.update)
	mux.HandleFunc("DELETE /environments/{name}", h.delete)
}

// environmentResponse is the wire representation of an environment.
type environmentResponse struct {
	Name        string     `json:"name"`
	Namespace   string     `json:"namespace"`
	DisplayName string     `json:"displayName"`
	Description string     `json:"description"`
	Default     bool       `json:"default"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	CreatedBy   *string    `json:"createdBy,omitempty"`
}

func toResponse(e Environment) environmentResponse {
	out := environmentResponse{
		Name:        e.Name,
		Namespace:   e.Namespace,
		DisplayName: e.DisplayName,
		Description: e.Description,
		Default:     e.IsDefault,
		CreatedBy:   e.CreatedBy,
	}
	if !e.CreatedAt.IsZero() {
		out.CreatedAt = &e.CreatedAt
	}
	return out
}

// createRequest is the body of a create request.
type createRequest struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
}

// updateRequest is the body of an update request. An omitted field is left as it is.
type updateRequest struct {
	DisplayName *string `json:"displayName,omitempty"`
	Description *string `json:"description,omitempty"`
}

// list godoc
//
//	@Summary		List environments
//	@Description	The default environment first, then every created one by name — those the
//	@Description	caller holds a role in: an environment-bound token sees its own.
//	@Tags			environments
//	@Produce		json
//	@Success		200	{array}	environmentResponse
//	@Router			/environments [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.List(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]environmentResponse, 0, len(items))
	for _, e := range items {
		if reaches(ctx, e.Name) {
			out = append(out, toResponse(e))
		}
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// reaches reports whether the caller holds a role in environment: one an
// environment-bound token was minted for, one a person was granted a role in, or
// any, for an administrator. A caller is shown only those, so a key bound to one
// environment is never offered another to pick and be refused in. Without a
// verified caller — enforcement off — every environment is reachable.
func reaches(ctx context.Context, environment string) bool {
	p, ok := authz.FromContext(ctx)
	return !ok || len(p.RolesIn(environment)) > 0
}

// get godoc
//
//	@Summary	Get an environment
//	@Tags		environments
//	@Produce	json
//	@Param		name	path		string	true	"Environment name"
//	@Success	200		{object}	environmentResponse
//	@Failure	404		{object}	httpx.ErrorResponse
//	@Router		/environments/{name} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	e, err := h.svc.Get(ctx, r.PathValue("name"))
	if err == nil && !reaches(ctx, e.Name) {
		err = ErrNotFound
	}
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(e))
}

// create godoc
//
//	@Summary		Create an environment
//	@Description	Provisions its namespace (octo-{name} unless one is given) with the RBAC
//	@Description	the orchestrator and runtime pods need there.
//	@Tags			environments
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createRequest	true	"The environment"
//	@Success		201		{object}	environmentResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"the name or namespace is taken"
//	@Failure		503		{object}	httpx.ErrorResponse	"this install cannot provision namespaces"
//	@Router			/environments [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	createdBy := ""
	if p, ok := authz.FromContext(ctx); ok {
		createdBy = p.Subject
	}
	e, err := h.svc.Create(ctx, Input(req), createdBy)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(e))
}

// update godoc
//
//	@Summary		Update an environment
//	@Description	Only the display name and description change; the name and namespace are fixed.
//	@Tags			environments
//	@Accept			json
//	@Produce		json
//	@Param			name	path		string			true	"Environment name"
//	@Param			body	body		updateRequest	true	"Fields to change"
//	@Success		200		{object}	environmentResponse
//	@Failure		404		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"the default environment"
//	@Router			/environments/{name} [patch]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	e, err := h.svc.Update(ctx, r.PathValue("name"), Patch(req))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(e))
}

// delete godoc
//
//	@Summary		Delete an environment
//	@Description	Deletes its namespace too. Refused while a deployment, test run or dev run
//	@Description	still runs there.
//	@Tags			environments
//	@Produce		json
//	@Param			name	path	string	true	"Environment name"
//	@Success		204		"deleted"
//	@Failure		404		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"the default environment, or workloads still run there"
//	@Router			/environments/{name} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("name")); err != nil {
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
		httpx.WriteError(w, http.StatusBadRequest,
			"invalid environment name (lowercase letters, digits and hyphens, at most 30; not \"octo\")")
	case errors.Is(err, ErrInvalidNamespace):
		httpx.WriteError(w, http.StatusBadRequest, "invalid namespace")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "environment not found")
	case errors.Is(err, ErrExists):
		httpx.WriteError(w, http.StatusConflict, "an environment with that name already exists")
	case errors.Is(err, ErrNamespaceTaken):
		httpx.WriteError(w, http.StatusConflict, "that namespace is already in use")
	case errors.Is(err, ErrDefault):
		httpx.WriteError(w, http.StatusConflict, "the default environment cannot be changed or deleted")
	case errors.Is(err, ErrInUse):
		httpx.WriteError(w, http.StatusConflict, "the environment still has integrations or workloads; delete them first")
	case errors.Is(err, ErrUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, "environments are not available on this installation")
	default:
		slog.Error("environment handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
