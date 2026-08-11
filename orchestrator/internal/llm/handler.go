package llm

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

// Handler serves the LLM settings endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the routes to mux. There is no route that returns the key, and
// none that calls the provider — this feature stores configuration for a consumer
// that does not exist yet.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/llm", h.get)
	mux.HandleFunc("PUT /settings/llm", h.update)
}

// settingsResponse is the wire representation. Like email's, it has no key field.
type settingsResponse struct {
	Provider   string     `json:"provider"`
	Model      string     `json:"model"`
	Configured bool       `json:"configured"`
	Last4      string     `json:"last4"`
	UpdatedAt  *time.Time `json:"updatedAt"`
	// EncryptionAvailable is not stored; it reports whether this orchestrator can
	// encrypt a key at all.
	EncryptionAvailable bool `json:"encryptionAvailable"`
}

// updateRequest is the body of a settings save. It converts directly to Update, so
// the field layouts have to match — if either gains a field the conversion stops
// compiling, which is the signal to write an explicit mapping.
type updateRequest struct {
	APIKey   *string `json:"apiKey"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
}

func (h *Handler) toResponse(s Settings) settingsResponse {
	return settingsResponse{
		Provider:            s.Provider,
		Model:               s.Model,
		Configured:          s.Configured,
		Last4:               s.Last4,
		UpdatedAt:           s.UpdatedAt,
		EncryptionAvailable: h.svc.EncryptionAvailable(),
	}
}

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
	case errors.Is(err, ErrInvalidProvider):
		httpx.WriteError(w, http.StatusBadRequest,
			"invalid provider (expected ANTHROPIC, OPENAI or GOOGLE)")
	case errors.Is(err, ErrInvalidModel):
		httpx.WriteError(w, http.StatusBadRequest, "invalid model")
	case errors.Is(err, ErrInvalidAPIKey):
		httpx.WriteError(w, http.StatusBadRequest, "invalid api key")
	case errors.Is(err, sitesettings.ErrNotConfigured):
		httpx.WriteError(w, http.StatusConflict, "no llm api key is configured")
	case errors.Is(err, sitesettings.ErrEncryptionUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"cannot store an llm api key: encryption is not configured (KV_ENCRYPTION_KEY)")
	default:
		slog.Error("llm handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
