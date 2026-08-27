package embedding

import (
	"context"
	"errors"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// requestTimeoutHTTP bounds the database work behind one settings request.
const requestTimeoutHTTP = 5 * time.Second

// Handler serves the site-wide embedding settings.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler serving svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the settings routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/embedding", h.get)
	mux.HandleFunc("PUT /settings/embedding", h.put)
	mux.HandleFunc("DELETE /settings/embedding", h.delete)
}

// get godoc
//
//	@Summary		Read the embedding settings
//	@Description	Which provider and model agent memory is vectorized with, and how far the backfill
//	@Description	has got. The API key is never returned — only whether one is stored and its last
//	@Description	four characters.
//	@Tags			settings
//	@Produce		json
//	@Success		200	{object}	Status
//	@Router			/settings/embedding [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeoutHTTP)
	defer cancel()

	status, err := h.svc.Status(ctx)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not read the embedding settings")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, status)
}

// put godoc
//
//	@Summary		Save the embedding settings
//	@Description	Omitting apiKey keeps the stored one, so changing the model does not destroy the
//	@Description	credentials — except when the provider changes, where a key that authenticates
//	@Description	against the old one is cleared rather than left reporting itself as configured.
//	@Description
//	@Description	Changing the MODEL with rows already embedded is a user error the platform does not
//	@Description	migrate for: stored vectors carry no record of which model produced them, so a table
//	@Description	holding two models' vectors is not searchable either way.
//	@Tags			settings
//	@Accept			json
//	@Produce		json
//	@Param			body	body		Update	true	"The settings"
//	@Success		200		{object}	Settings
//	@Failure		400		{object}	httpx.ErrorResponse	"the provider, model or key is not usable"
//	@Failure		503		{object}	httpx.ErrorResponse	"a key was supplied with no encryption configured"
//	@Router			/settings/embedding [put]
func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	var body Update
	if err := httpx.DecodeJSON(w, r, &body); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeoutHTTP)
	defer cancel()

	settings, err := h.svc.Update(ctx, body)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, settings)
}

// delete godoc
//
//	@Summary		Turn embeddings off
//	@Description	Clears the provider, model and key. Stored vectors are left alone: they cost nothing
//	@Description	where they are, and turning the same model back on should not mean re-embedding a
//	@Description	whole history. Search falls back to full text immediately.
//	@Tags			settings
//	@Success		204	"cleared"
//	@Router			/settings/embedding [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeoutHTTP)
	defer cancel()

	if err := h.svc.Clear(ctx); err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "could not clear the embedding settings")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps the package's sentinels onto status codes.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidProvider):
		httpx.WriteError(w, http.StatusBadRequest,
			"choose a provider with an embeddings endpoint (Anthropic has none)")
	case errors.Is(err, ErrInvalidModel):
		httpx.WriteError(w, http.StatusBadRequest, "the model identifier is not usable")
	case errors.Is(err, ErrInvalidAPIKey):
		httpx.WriteError(w, http.StatusBadRequest, "that does not look like an API key")
	default:
		if !h.svc.EncryptionAvailable() {
			httpx.WriteError(w, http.StatusServiceUnavailable,
				"no encryption key is configured, so an API key cannot be stored")
			return
		}
		httpx.WriteError(w, http.StatusInternalServerError, "could not save the embedding settings")
	}
}
