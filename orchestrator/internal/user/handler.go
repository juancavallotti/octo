package user

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

// Handler serves the user REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the user routes to mux. The platform calls bootstrap from its
// sign-in callback; the GET is for diagnostics.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /users/bootstrap", h.bootstrap)
	mux.HandleFunc("GET /users/{id}", h.get)
}

// bootstrapRequest is the first-sign-in payload: the OIDC subject plus the
// metadata to keep in sync.
type bootstrapRequest struct {
	Subject string `json:"subject"`
	Email   string `json:"email"`
	Name    string `json:"name"`
}

// userResponse is the wire representation of a user. It carries the durable id the
// platform stores on the session; the subject stays internal.
type userResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"createdAt"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

func toResponse(u User) userResponse {
	return userResponse{
		ID:          u.ID,
		Email:       u.Email,
		Name:        u.Name,
		CreatedAt:   u.CreatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

// bootstrap godoc
//
//	@Summary		Resolve a signed-in identity to an octo user
//	@Description	Called on sign-in. Creates the user on first sight and returns the same durable id
//	@Description	on every later call, so the platform has one identity to attribute writes to
//	@Description	regardless of what the identity provider changes about the account.
//	@Tags			users
//	@Accept			json
//	@Produce		json
//	@Param			body	body		bootstrapRequest	true	"Subject, email and display name"
//	@Success		200		{object}	userResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Router			/users/bootstrap [post]
func (h *Handler) bootstrap(w http.ResponseWriter, r *http.Request) {
	var req bootstrapRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	u, err := h.svc.Bootstrap(ctx, req.Subject, req.Email, req.Name)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(u))
}

// get godoc
//
//	@Summary	Get a user
//	@Tags		users
//	@Produce	json
//	@Param		id	path		string	true	"User id"
//	@Success	200	{object}	userResponse
//	@Failure	404	{object}	httpx.ErrorResponse
//	@Router		/users/{id} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	u, err := h.svc.Get(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(u))
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are logged
// and reported generically so internals do not leak to clients.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "user not found")
	default:
		slog.Error("user handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
