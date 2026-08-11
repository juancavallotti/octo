package resource

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

// Handler serves the resource REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the resource routes to mux. Every route is nested under the
// owning integration: a resource only exists within an integration, so its id
// alone is not a valid address and the nested path is authoritative (a resource
// addressed under the wrong integration reads as not found).
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations/{id}/resources", h.create)
	mux.HandleFunc("GET /integrations/{id}/resources", h.listByIntegration)
	mux.HandleFunc("GET /integrations/{id}/resources/{resourceId}", h.get)
	mux.HandleFunc("PUT /integrations/{id}/resources/{resourceId}", h.update)
	mux.HandleFunc("DELETE /integrations/{id}/resources/{resourceId}", h.delete)
}

// resourceRequest is the create/update payload.
type resourceRequest struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

// resourceResponse is the wire representation of a resource.
type resourceResponse struct {
	ID            string    `json:"id"`
	IntegrationID string    `json:"integrationId"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	Content       string    `json:"content"`
	CreatedAt     time.Time `json:"createdAt"`
	LastUpdated   time.Time `json:"lastUpdated"`
}

// toResponse maps the domain model to its wire form. The field layouts match,
// so a direct conversion suffices; if they diverge this stops compiling, which
// is the signal to write an explicit mapping.
func toResponse(res Resource) resourceResponse {
	return resourceResponse(res)
}

// create godoc
//
//	@Summary		Add a resource to an integration
//	@Description	Resources are the files a definition refers to: env files and templates. Names are
//	@Description	relative paths and may contain slashes.
//	@Tags			resources
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Integration id"
//	@Param			body	body		resourceRequest	true	"Kind, name and content"
//	@Success		201		{object}	resourceResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"an unknown kind or an unsafe name"
//	@Failure		409		{object}	httpx.ErrorResponse	"the name is taken"
//	@Router			/integrations/{id}/resources [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req resourceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	res, err := h.svc.Create(ctx, r.PathValue("id"), req.Kind, req.Name, req.Content)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(res))
}

// listByIntegration godoc
//
//	@Summary		List an integration's resources
//	@Tags			resources
//	@Produce		json
//	@Param			id	path		string	true	"Integration id"
//	@Success		200	{array}		resourceResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/integrations/{id}/resources [get]
func (h *Handler) listByIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.ListByIntegration(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]resourceResponse, 0, len(items))
	for _, res := range items {
		out = append(out, toResponse(res))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// get godoc
//
//	@Summary	Get a resource
//	@Tags		resources
//	@Produce	json
//	@Param		id			path		string	true	"Integration id"
//	@Param		resourceId	path		string	true	"Resource id"
//	@Success	200			{object}	resourceResponse
//	@Failure	404			{object}	httpx.ErrorResponse
//	@Router		/integrations/{id}/resources/{resourceId} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	res, err := h.svc.Get(ctx, r.PathValue("id"), r.PathValue("resourceId"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(res))
}

// update godoc
//
//	@Summary	Replace a resource
//	@Tags		resources
//	@Accept		json
//	@Produce	json
//	@Param		id			path		string			true	"Integration id"
//	@Param		resourceId	path		string			true	"Resource id"
//	@Param		body		body		resourceRequest	true	"Kind, name and content"
//	@Success	200			{object}	resourceResponse
//	@Failure	400			{object}	httpx.ErrorResponse
//	@Failure	404			{object}	httpx.ErrorResponse
//	@Failure	409			{object}	httpx.ErrorResponse	"the name is taken"
//	@Router		/integrations/{id}/resources/{resourceId} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req resourceRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	res, err := h.svc.Update(ctx, r.PathValue("id"), r.PathValue("resourceId"), req.Kind, req.Name, req.Content)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(res))
}

// delete godoc
//
//	@Summary	Delete a resource
//	@Tags		resources
//	@Produce	json
//	@Param		id			path	string	true	"Integration id"
//	@Param		resourceId	path	string	true	"Resource id"
//	@Success	204			"deleted"
//	@Failure	404			{object}	httpx.ErrorResponse
//	@Router		/integrations/{id}/resources/{resourceId} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("id"), r.PathValue("resourceId")); err != nil {
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
	case errors.Is(err, ErrNameExists):
		httpx.WriteError(w, http.StatusConflict, "a resource with this name already exists")
	case errors.Is(err, ErrIntegrationNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "resource not found")
	default:
		slog.Error("resource handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
