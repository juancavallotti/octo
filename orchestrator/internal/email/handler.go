package email

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
	"github.com/juancavallotti/octo/orchestrator/internal/mailer"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

const (
	// requestTimeout bounds the database work behind a settings read or write.
	requestTimeout = 5 * time.Second
	// sendTimeout bounds a send. Longer than the mailer's own bound on purpose, so
	// the provider timeout is the one that fires and the caller gets "provider
	// unavailable" rather than a bare context deadline.
	sendTimeout = 20 * time.Second
)

// Handler serves the email settings and send endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the routes to mux.
//
// The send endpoint sits outside /settings on purpose: sending is an operation, not
// a setting, and putting it under /settings/email/ would read as though it configured
// something.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/email", h.get)
	mux.HandleFunc("PUT /settings/email", h.update)
	mux.HandleFunc("POST /settings/email/test", h.test)
	mux.HandleFunc("POST /email/send", h.send)
}

// settingsResponse is the wire representation. It has no key field — the closest it
// comes is last4, which exists so a UI can say which key is stored.
type settingsResponse struct {
	FromEmail  string     `json:"fromEmail"`
	FromName   string     `json:"fromName"`
	ReplyTo    string     `json:"replyTo"`
	Configured bool       `json:"configured"`
	Last4      string     `json:"last4"`
	UpdatedAt  *time.Time `json:"updatedAt"`
	// EncryptionAvailable is not stored; it reports whether this orchestrator can
	// encrypt a key at all, so the UI can explain a disabled input rather than
	// letting the operator discover it when the save fails.
	EncryptionAvailable bool `json:"encryptionAvailable"`
}

// updateRequest is the body of a settings save. APIKey is a pointer so an omitted
// field ("keep the stored key") is distinguishable from an empty one ("remove it").
//
// This and the two request types below convert directly to their domain
// counterparts, so their field layouts have to match. That is the point: if either
// side gains a field the conversion stops compiling, which is the signal to write an
// explicit mapping rather than to discover a silently dropped value at runtime.
type updateRequest struct {
	APIKey    *string `json:"apiKey"`
	FromEmail string  `json:"fromEmail"`
	FromName  string  `json:"fromName"`
	ReplyTo   string  `json:"replyTo"`
}

// testRequest is the body of a test send: the draft on screen, plus a recipient.
type testRequest struct {
	To        string  `json:"to"`
	APIKey    *string `json:"apiKey"`
	FromEmail string  `json:"fromEmail"`
	FromName  string  `json:"fromName"`
	ReplyTo   string  `json:"replyTo"`
}

// sendRequestBody is the body of a platform-internal send. There is deliberately no
// apiKey or from field: DecodeJSON rejects unknown fields, so a caller trying to
// supply either gets a 400 from the decoder rather than a silently ignored value.
type sendRequestBody struct {
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
	HTML    string   `json:"html"`
}

// sendResponse carries the provider's id for the queued message.
type sendResponse struct {
	MessageID string `json:"messageId"`
}

func (h *Handler) toResponse(s Settings) settingsResponse {
	return settingsResponse{
		FromEmail:           s.FromEmail,
		FromName:            s.FromName,
		ReplyTo:             s.ReplyTo,
		Configured:          s.Configured,
		Last4:               s.Last4,
		UpdatedAt:           s.UpdatedAt,
		EncryptionAvailable: h.svc.EncryptionAvailable(),
	}
}

// get godoc
//
//	@Summary		Get the site's email settings
//	@Description	The from identity and whether a provider key is stored. The key is never returned,
//	@Description	only its last four characters.
//	@Tags			settings
//	@Produce		json
//	@Success		200	{object}	settingsResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/settings/email [get]
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
//	@Summary		Save the site's email settings
//	@Description	apiKey is three-state: omitted keeps the stored key, an empty string removes it, and
//	@Description	any other value replaces it. Storing one requires encryption to be configured.
//	@Tags			settings
//	@Accept			json
//	@Produce		json
//	@Param			body	body		updateRequest	true	"From identity and optionally the key"
//	@Success		200		{object}	settingsResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"an invalid address or key"
//	@Failure		503		{object}	httpx.ErrorResponse	"encryption is not configured"
//	@Router			/settings/email [put]
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

// test godoc
//
//	@Summary		Send a test message
//	@Description	Uses the draft in the request rather than what is stored, so a configuration can be
//	@Description	proved before it is saved; a supplied key wins over the stored one, and the stored
//	@Description	one is the fallback after a reload.
//	@Tags			settings
//	@Accept			json
//	@Produce		json
//	@Param			body	body		testRequest	true	"Recipient and the draft settings"
//	@Success		200		{object}	sendResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"the provider rejected the key or the from address"
//	@Failure		409		{object}	httpx.ErrorResponse	"no key supplied and none stored"
//	@Failure		502		{object}	httpx.ErrorResponse	"the provider was unreachable"
//	@Router			/settings/email/test [post]
func (h *Handler) test(w http.ResponseWriter, r *http.Request) {
	var req testRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), sendTimeout)
	defer cancel()

	id, err := h.svc.SendTest(ctx, TestSend(req))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sendResponse{MessageID: id})
}

// send godoc
//
//	@Summary		Send an email
//	@Description	The operation in-cluster callers use. The key and the from identity come from the
//	@Description	stored settings and the request carries neither — a body supplying one is rejected
//	@Description	by the decoder, which is what keeps this from being a relay for whatever key a
//	@Description	caller holds. Recipients are capped, and CR/LF is refused in the subject.
//	@Tags			email
//	@Accept			json
//	@Produce		json
//	@Param			body	body		sendRequestBody	true	"Recipients, subject and body"
//	@Success		200		{object}	sendResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"invalid recipients, subject or body"
//	@Failure		409		{object}	httpx.ErrorResponse	"email is not configured"
//	@Failure		502		{object}	httpx.ErrorResponse	"the provider was unreachable"
//	@Router			/email/send [post]
func (h *Handler) send(w http.ResponseWriter, r *http.Request) {
	var req sendRequestBody
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), sendTimeout)
	defer cancel()

	id, err := h.svc.Send(ctx, SendRequest(req))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, sendResponse{MessageID: id})
}

// writeError maps domain errors to HTTP status codes. These strings reach the
// operator verbatim — the BFF passes the error envelope straight through — so they
// are written to be read, not to be parsed.
//
// The two provider errors below are 400 rather than 502 because in both cases the
// fix is in the caller's input: the key they pasted, or the from-address they chose.
// Letting either fall into the default branch is what would make a bad API key read
// as "internal error".
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidFromEmail):
		httpx.WriteError(w, http.StatusBadRequest, "invalid from address (use a bare address; put the display name in fromName)")
	case errors.Is(err, ErrInvalidReplyTo):
		httpx.WriteError(w, http.StatusBadRequest, "invalid reply-to address")
	case errors.Is(err, ErrInvalidFromName):
		httpx.WriteError(w, http.StatusBadRequest, "invalid from name")
	case errors.Is(err, ErrInvalidAPIKey):
		httpx.WriteError(w, http.StatusBadRequest, "invalid api key")
	case errors.Is(err, ErrInvalidRecipient):
		httpx.WriteError(w, http.StatusBadRequest, "invalid recipient address")
	case errors.Is(err, ErrInvalidRecipients):
		httpx.WriteError(w, http.StatusBadRequest, "invalid recipients")
	case errors.Is(err, ErrInvalidSubject):
		httpx.WriteError(w, http.StatusBadRequest, "invalid subject")
	case errors.Is(err, ErrEmptyBody):
		httpx.WriteError(w, http.StatusBadRequest, "message body is empty")
	case errors.Is(err, sitesettings.ErrNotConfigured):
		httpx.WriteError(w, http.StatusConflict, "email is not configured")
	case errors.Is(err, sitesettings.ErrEncryptionUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"cannot store an email api key: encryption is not configured (KV_ENCRYPTION_KEY)")
	case errors.Is(err, mailer.ErrInvalidAPIKey):
		httpx.WriteError(w, http.StatusBadRequest, "invalid Resend API key: "+err.Error())
	case errors.Is(err, mailer.ErrRejected):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, mailer.ErrUnavailable):
		httpx.WriteError(w, http.StatusBadGateway, "email provider unavailable")
	default:
		slog.Error("email handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
