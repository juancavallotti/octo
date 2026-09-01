package websearch

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// requestTimeout bounds the database work behind a settings read or write.
const requestTimeout = 5 * time.Second

// Handler serves the web search settings endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the routes to mux. As with the LLM settings there is no route
// that returns the key, and none that calls the provider.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/websearch", h.get)
	mux.HandleFunc("PUT /settings/websearch", h.update)
}

// settingsResponse is the wire representation. Like the LLM one, it has no key
// field — only whether one is stored and its last four characters.
type settingsResponse struct {
	// Provider is constant today. It is sent anyway so the page can name what the
	// key is for without hard-coding it, and so adding a second provider later is a
	// change to this response rather than to its contract.
	Provider   string     `json:"provider"`
	Configured bool       `json:"configured"`
	Last4      string     `json:"last4"`
	UpdatedAt  *time.Time `json:"updatedAt"`
	// EncryptionAvailable is not stored; it reports whether this orchestrator can
	// encrypt a key at all.
	EncryptionAvailable bool `json:"encryptionAvailable"`
}

// updateRequest is the body of a settings save.
type updateRequest struct {
	APIKey *string `json:"apiKey"`
}

func (h *Handler) toResponse(s Settings) settingsResponse {
	return settingsResponse{
		Provider:            s.Provider,
		Configured:          s.Configured,
		Last4:               s.Last4,
		UpdatedAt:           s.UpdatedAt,
		EncryptionAvailable: h.svc.EncryptionAvailable(),
	}
}

// get godoc
//
//	@Summary		Get the site's web search settings
//	@Description	Whether a Parallel API key is stored, and which one — its last four
//	@Description	characters only. The key itself is never returned. With no key stored the
//	@Description	platform agent still runs; his web_search tool reports itself unavailable.
//	@Tags			settings
//	@Produce		json
//	@Success		200	{object}	settingsResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/settings/websearch [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	settings, err := h.svc.Get(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toResponse(settings))
}

// update godoc
//
//	@Summary		Save the site's web search settings
//	@Description	apiKey is three-state: omitted keeps the stored key, an empty string
//	@Description	removes it, and any other value replaces it. Storing a key requires the
//	@Description	orchestrator to have an encryption key configured; without one the save is
//	@Description	refused rather than performed in the clear. A change reaches the agent on
//	@Description	his next roll-out, which is where the deployment's bindings are written.
//	@Tags			settings
//	@Accept			json
//	@Produce		json
//	@Param			body	body		updateRequest	true	"The Parallel API key"
//	@Success		200		{object}	settingsResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"invalid key"
//	@Failure		503		{object}	httpx.ErrorResponse	"encryption is not configured"
//	@Router			/settings/websearch [put]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	settings, err := h.svc.Update(ctx, Update(req))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, h.toResponse(settings))
}

// writeError maps domain errors to HTTP status codes. These strings reach the
// operator verbatim — the BFF passes the error envelope straight through.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidAPIKey):
		httpx.WriteError(w, http.StatusBadRequest, "invalid api key")
	case errors.Is(err, sitesettings.ErrEncryptionUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"cannot store a web search api key: encryption is not configured (KV_ENCRYPTION_KEY)")
	default:
		slog.Error("websearch handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
