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

// integrationRequest is the create/update payload. ActorID is the acting user's
// id, forwarded by the BFF from the authenticated session (empty when unknown);
// the orchestrator trusts the BFF as the auth boundary, so it is a body field
// rather than a verified credential.
type integrationRequest struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	ActorID    string `json:"actorId"`
}

// integrationResponse is the wire representation of an integration. The
// attribution fields are omitted when unknown. Its field layout mirrors the
// domain Integration so toResponse is a direct conversion.
type integrationResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Definition  string    `json:"definition"`
	LastUpdated time.Time `json:"lastUpdated"`

	CreatedBy      *string `json:"createdBy,omitempty"`
	UpdatedBy      *string `json:"updatedBy,omitempty"`
	CreatedByEmail *string `json:"createdByEmail,omitempty"`
	CreatedByName  *string `json:"createdByName,omitempty"`
	UpdatedByEmail *string `json:"updatedByEmail,omitempty"`
	UpdatedByName  *string `json:"updatedByName,omitempty"`
}

// toResponse maps the domain model to its wire form. The field layouts match,
// so a direct conversion suffices; if they diverge this stops compiling, which
// is the signal to write an explicit mapping.
func toResponse(it Integration) integrationResponse {
	return integrationResponse(it)
}

// create godoc
//
//	@Summary		Create an integration
//	@Description	Stores a new integration definition under a name unique across the install (case-insensitively).
//	@Tags			integrations
//	@Accept			json
//	@Produce		json
//	@Param			body	body		integrationRequest	true	"Name and definition"
//	@Success		201		{object}	integrationResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"invalid name or body"
//	@Failure		409		{object}	httpx.ErrorResponse	"the name is taken"
//	@Router			/integrations [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req integrationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	it, err := h.svc.Create(ctx, req.Name, req.Definition, req.ActorID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(it))
}

// list godoc
//
//	@Summary		List integrations
//	@Description	Every integration in the install, each with its full definition.
//	@Tags			integrations
//	@Produce		json
//	@Success		200	{array}		integrationResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/integrations [get]
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

// get godoc
//
//	@Summary		Get an integration
//	@Tags			integrations
//	@Produce		json
//	@Param			id	path		string	true	"Integration id"
//	@Success		200	{object}	integrationResponse
//	@Failure		404	{object}	httpx.ErrorResponse
//	@Router			/integrations/{id} [get]
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

// update godoc
//
//	@Summary		Replace an integration
//	@Description	Overwrites the name and definition. This is the live working copy, not a
//	@Description	version tag: a running deployment keeps serving its own frozen snapshot
//	@Description	until it is rolled out.
//	@Tags			integrations
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string				true	"Integration id"
//	@Param			body	body		integrationRequest	true	"Name and definition"
//	@Success		200		{object}	integrationResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		404		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"the name is taken"
//	@Router			/integrations/{id} [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req integrationRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	it, err := h.svc.Update(ctx, r.PathValue("id"), req.Name, req.Definition, req.ActorID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(it))
}

// delete godoc
//
//	@Summary	Delete an integration
//	@Tags		integrations
//	@Produce	json
//	@Param		id	path	string	true	"Integration id"
//	@Success	204	"deleted"
//	@Failure	404	{object}	httpx.ErrorResponse
//	@Router		/integrations/{id} [delete]
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
	case errors.Is(err, ErrNameTaken):
		httpx.WriteError(w, http.StatusConflict, "an integration with this name already exists")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	default:
		slog.Error("integration handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
