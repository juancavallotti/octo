package apikey

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/iam/internal/authz"
	httpx "github.com/juancavallotti/octo/iam/internal/http"
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

// Middleware wraps a handler with whatever a caller must satisfy to reach it.
type Middleware func(http.Handler) http.Handler

// Register attaches the API-key routes to mux behind signedIn.
//
// The keys stay nested under the user they belong to, and the user in the path
// must be the caller: nobody, an administrator included, mints, reads or revokes
// another person's keys. A key is a standing credential that acts as its owner,
// so handing one out on somebody else's behalf would be impersonating them.
func (h *Handler) Register(mux *http.ServeMux, signedIn Middleware) {
	for pattern, handler := range map[string]http.HandlerFunc{
		"POST /users/{userId}/apikeys":        h.create,
		"GET /users/{userId}/apikeys":         h.list,
		"DELETE /users/{userId}/apikeys/{id}": h.delete,
	} {
		mux.Handle(pattern, signedIn(ownerOnly(handler)))
	}
}

// ownerOnly refuses a caller who is not the user named in the path, any machine
// token, and any token that was itself exchanged for a key.
//
// A deployment's token has its owner as subject, so without the second check a
// running integration could mint itself a key — trading the narrow access it was
// lent for a year-long credential carrying everything its owner may do. The third
// keeps a key from minting its own successor, which would let a leaked key
// survive being revoked.
func ownerOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := authz.FromContext(r.Context())
		if err != nil {
			slog.Error("apikey route reached without a guard", "error", err)
			httpx.WriteError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if p.Machine {
			httpx.WriteError(w, http.StatusForbidden, "a deployment cannot manage API keys")
			return
		}
		if p.FromAPIKey {
			httpx.WriteError(w, http.StatusForbidden, "API keys are managed from a signed-in session, not with a key")
			return
		}
		if p.Subject != r.PathValue("userId") {
			httpx.WriteError(w, http.StatusForbidden, "API keys can only be managed by the user they belong to")
			return
		}
		next(w, r)
	}
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
	// The one response carrying the plaintext, so nothing in between may keep it.
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusCreated, createResponse{
		apiKeyResponse: toResponse(k),
		Token:          token,
	})
}

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

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("id"), r.PathValue("userId")); err != nil {
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
		httpx.WriteError(w, http.StatusBadRequest, "invalid api key name")
	case errors.Is(err, ErrInvalidTTL):
		httpx.WriteError(w, http.StatusBadRequest, "invalid api key expiration")
	case errors.Is(err, ErrUserNotFound):
		httpx.WriteError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "api key not found")
	default:
		slog.Error("apikey handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
