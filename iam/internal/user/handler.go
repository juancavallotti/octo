package user

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/iam/internal/http"
)

// requestTimeout bounds the database work behind a single request.
const requestTimeout = 5 * time.Second

// Handler serves the user and role-grant REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the user routes to mux.
//
// Role grants stay nested under the user they belong to, following the same rule
// the orchestrator's per-integration resources and per-user API keys follow: a
// sub-entity is addressed through its owner, so there is never a second way to
// name the same thing.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /roles", h.catalogue)
	mux.HandleFunc("GET /users", h.list)
	mux.HandleFunc("GET /users/{id}", h.get)
	mux.HandleFunc("PUT /users/{id}/roles/{role}", h.grant)
	mux.HandleFunc("DELETE /users/{id}/roles/{role}", h.revoke)
}

// Response is the wire representation of a user. It carries the durable id every
// other table references and the roles a caller is entitled to; the OIDC subject
// stays internal, because nothing outside this service has any use for it and it
// identifies the account at the identity provider.
//
// Exported because the token exchange renders the same shape inside its own
// response, and two structs describing one user is how they come to disagree.
type Response struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	Name        string    `json:"name"`
	Roles       []Role    `json:"roles"`
	CreatedAt   time.Time `json:"createdAt"`
	LastLoginAt time.Time `json:"lastLoginAt"`
}

// ToResponse renders u for the wire.
func ToResponse(u User) Response {
	roles := u.Roles
	if roles == nil {
		// An absent list and an empty one mean the same thing here, and `null`
		// makes a caller handle a case that never carries information.
		roles = []Role{}
	}
	return Response{
		ID:          u.ID,
		Email:       u.Email,
		Name:        u.Name,
		Roles:       roles,
		CreatedAt:   u.CreatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

// roleResponse is one entry of the catalogue.
type roleResponse struct {
	Role        Role   `json:"role"`
	Description string `json:"description"`
}

// catalogue lists the grantable roles. It is served from the code's own
// catalogue rather than from the distinct values in user_roles, so a role nobody
// holds yet is still offered.
func (h *Handler) catalogue(w http.ResponseWriter, _ *http.Request) {
	roles := AllRoles()
	out := make([]roleResponse, 0, len(roles))
	for _, r := range roles {
		out = append(out, roleResponse{Role: r, Description: DescribeRole(r)})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	users, err := h.svc.List(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]Response, 0, len(users))
	for _, u := range users {
		out = append(out, ToResponse(u))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	u, err := h.svc.Get(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToResponse(u))
}

// grant is a PUT and not a POST because it names the grant it creates: the same
// request twice leaves the same state, and the role is in the path rather than in
// a body.
//
// It answers with the user, so a caller that just changed what someone may do
// sees the whole set rather than having to read it back.
func (h *Handler) grant(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	id := r.PathValue("id")
	// grantedBy is nil until the wiring change puts an authenticated caller behind
	// these routes; the column is nullable for exactly this reason, and recording
	// a guess would be worse than recording nothing.
	if err := h.svc.Grant(ctx, id, Role(r.PathValue("role")), nil); err != nil {
		h.writeError(w, err)
		return
	}
	h.respondWithUser(ctx, w, id)
}

func (h *Handler) revoke(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	id := r.PathValue("id")
	if err := h.svc.Revoke(ctx, id, Role(r.PathValue("role"))); err != nil {
		h.writeError(w, err)
		return
	}
	h.respondWithUser(ctx, w, id)
}

// respondWithUser reads the user back and writes them, which both grant and
// revoke do once their write lands.
func (h *Handler) respondWithUser(ctx context.Context, w http.ResponseWriter, id string) {
	u, err := h.svc.Get(ctx, id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToResponse(u))
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are
// logged and reported generically so internals do not leak to clients.
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
