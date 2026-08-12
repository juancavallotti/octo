package apikey

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

// Handler serves the API-key REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the API-key routes to mux. The owner-scoped CRUD routes are
// nested under the user; verify is unscoped because it discovers the owner from
// the token.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /users/{userId}/apikeys", h.create)
	mux.HandleFunc("GET /users/{userId}/apikeys", h.list)
	mux.HandleFunc("DELETE /users/{userId}/apikeys/{id}", h.delete)
	mux.HandleFunc("POST /apikeys/verify", h.verify)
}

// createRequest is the body of a create request.
type createRequest struct {
	Name       string `json:"name"`
	TTLSeconds int64  `json:"ttlSeconds"`
}

// apiKeyResponse is the wire representation of a key. It carries no secret
// material — only the non-secret display fragments.
type apiKeyResponse struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Prefix     string     `json:"prefix"`
	Last4      string     `json:"last4"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  time.Time  `json:"expiresAt"`
	LastUsedAt *time.Time `json:"lastUsedAt"`
}

// createResponse adds the one-time plaintext token to the key metadata. This is
// the only response that ever carries the token.
type createResponse struct {
	apiKeyResponse
	Token string `json:"token"`
}

// verifyRequest carries the bearer token to resolve.
type verifyRequest struct {
	Token string `json:"token"`
}

// verifyResponse identifies the owning user behind a valid token.
type verifyResponse struct {
	ID     string `json:"id"`
	UserID string `json:"userId"`
	Name   string `json:"name"`
}

func toResponse(k APIKey) apiKeyResponse {
	return apiKeyResponse{
		ID:         k.ID,
		Name:       k.Name,
		Prefix:     k.Prefix,
		Last4:      k.Last4,
		CreatedAt:  k.CreatedAt,
		ExpiresAt:  k.ExpiresAt,
		LastUsedAt: k.LastUsedAt,
	}
}

// create godoc
//
//	@Summary		Mint an API key for a user
//	@Description	The only response that ever carries the token: it is hashed on the way in and
//	@Description	cannot be read back, so a caller that loses it mints another.
//	@Tags			apikeys
//	@Accept			json
//	@Produce		json
//	@Param			userId	path		string			true	"User id"
//	@Param			body	body		createRequest	true	"Name and lifetime in seconds"
//	@Success		201		{object}	createResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Router			/users/{userId}/apikeys [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	k, token, err := h.svc.Create(ctx, r.PathValue("userId"), req.Name,
		time.Duration(req.TTLSeconds)*time.Second)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, createResponse{
		apiKeyResponse: toResponse(k),
		Token:          token,
	})
}

// list godoc
//
//	@Summary		List a user's API keys
//	@Description	Metadata only — prefix and last four characters, never the token.
//	@Tags			apikeys
//	@Produce		json
//	@Param			userId	path		string	true	"User id"
//	@Success		200		{array}		apiKeyResponse
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/users/{userId}/apikeys [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.List(ctx, r.PathValue("userId"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]apiKeyResponse, 0, len(items))
	for _, k := range items {
		out = append(out, toResponse(k))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// delete godoc
//
//	@Summary	Revoke an API key
//	@Tags		apikeys
//	@Produce	json
//	@Param		userId	path	string	true	"User id"
//	@Param		id		path	string	true	"API key id"
//	@Success	204		"revoked"
//	@Failure	404		{object}	httpx.ErrorResponse
//	@Router		/users/{userId}/apikeys/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("id"), r.PathValue("userId")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// verify godoc
//
//	@Summary		Resolve a bearer token to its owner
//	@Description	How a caller holding an octo API key is identified. An expired or unknown token is
//	@Description	a 401 rather than a 404, so an attacker cannot tell the two apart.
//	@Tags			apikeys
//	@Accept			json
//	@Produce		json
//	@Param			body	body		verifyRequest	true	"The token to verify"
//	@Success		200		{object}	verifyResponse
//	@Failure		401		{object}	httpx.ErrorResponse	"unknown or expired"
//	@Router			/apikeys/verify [post]
func (h *Handler) verify(w http.ResponseWriter, r *http.Request) {
	var req verifyRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	k, err := h.svc.Verify(ctx, req.Token)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, verifyResponse{ID: k.ID, UserID: k.UserID, Name: k.Name})
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are logged
// and reported generically so internals do not leak to clients.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidName):
		httpx.WriteError(w, http.StatusBadRequest, "invalid api key name")
	case errors.Is(err, ErrInvalidTTL):
		httpx.WriteError(w, http.StatusBadRequest, "invalid api key expiration")
	case errors.Is(err, ErrUserNotFound):
		httpx.WriteError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "api key not found")
	case errors.Is(err, ErrExpired), errors.Is(err, ErrInvalidToken):
		httpx.WriteError(w, http.StatusUnauthorized, "invalid or expired api key")
	default:
		slog.Error("apikey handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
