package user

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/juancavallotti/octo/iam/internal/authz"
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

// Middleware wraps a handler with whatever a caller must satisfy to reach it.
type Middleware func(http.Handler) http.Handler

// Register attaches the user routes to mux, each behind the guard its contents
// call for.
//
// The guards arrive as arguments rather than being built here, because this
// package must not decide who may administer the platform on top of describing
// what administering it looks like — and because handing them in is what lets
// the caller refuse to register any of this at all when it has no way to check a
// token. See newServer.
//
// Role grants stay nested under the user they belong to, following the same rule
// the orchestrator's per-integration resources and per-user API keys follow: a
// sub-entity is addressed through its owner, so there is never a second way to
// name the same thing.
func (h *Handler) Register(mux *http.ServeMux, signedIn, admin Middleware) {
	// The catalogue is a list of four constants and describes nothing about this
	// installation, so it asks only that the caller be somebody.
	mux.Handle("GET /roles", signedIn(http.HandlerFunc(h.catalogue)))

	// Everything else is the user directory and what people may do, which is an
	// administrator's business and nobody else's.
	for pattern, handler := range map[string]http.HandlerFunc{
		"POST /users":                     h.create,
		"GET /users":                      h.list,
		"GET /users/{id}":                 h.get,
		"PUT /users/{id}":                 h.update,
		"DELETE /users/{id}":              h.delete,
		"PUT /users/{id}/roles/{role}":    h.grant,
		"DELETE /users/{id}/roles/{role}": h.revoke,
	} {
		mux.Handle(pattern, admin(handler))
	}
}

// Response is the wire representation of a user: the durable id every other table
// references, the roles they hold, and the subject their identity provider
// presents.
//
// The subject is here for one reason, and it is worth naming so it is not
// mistaken for something to key on. It is the answer to "why is this person not
// getting in" — whether their row has been claimed yet, and by which account at
// the provider. Nothing addresses a user by it outside this service.
//
// Exported because the token exchange renders the same shape inside its own
// response, and two structs describing one user is how they come to disagree.
type Response struct {
	ID string `json:"id"`
	// Subject is empty for somebody provisioned who has not signed in yet.
	Subject   string    `json:"subject"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Roles     []Role    `json:"roles"`
	CreatedAt time.Time `json:"createdAt"`
	// LastLoginAt is null for somebody provisioned who has not arrived yet, which
	// is why it is a pointer: zero time would render as a date in the year 1 and
	// read as data.
	LastLoginAt *time.Time `json:"lastLoginAt"`
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
		Subject:     u.Subject,
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

// createRequest is a user an administrator is adding. No subject: nobody types
// one, it is written by the first sign-in. Roles arrive with the person because
// letting somebody in and saying what they may do is one decision.
type createRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Roles []Role `json:"roles,omitempty"`
}

// updateRequest is the profile an administrator is correcting. The subject is
// absent on purpose — it is what the row is keyed by once the first sign-in has
// discovered it, so changing it would point the account at somebody else.
type updateRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// create adds a user before they have ever signed in, which is how somebody is
// let in at all: this platform admits only provisioned users.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	// Who granted the roles that come with them, recorded the same way a later
	// grant records it. Only nil-able if this route were mounted without its
	// guard, which would be a wiring mistake rather than a caller's.
	caller, err := authz.FromContext(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	u, err := h.svc.Create(ctx, req.Email, req.Name, req.Roles, &caller.Subject)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ToResponse(u))
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	u, err := h.svc.Update(ctx, r.PathValue("id"), req.Email, req.Name)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ToResponse(u))
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// page is one screenful of the directory, with the cursor for the next.
//
// An envelope rather than a bare array, because the cursor has to travel with
// the rows and a header would put half the answer somewhere a JSON client is not
// looking. `nextCursor` is absent on the last page, which is what a caller checks
// rather than comparing counts against the limit it asked for.
type page struct {
	Items      []Response `json:"items"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// list serves one page of the directory, filtered by `q` over name and address
// and by `role` over what people hold.
//
// A limit that is not a number is the default rather than a refusal: the
// parameter is a hint about page size, and failing a listing over it would be a
// worse answer to a typo than serving a screenful.
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	users, next, err := h.svc.List(ctx,
		r.URL.Query().Get("q"),
		Role(r.URL.Query().Get("role")),
		limit,
		r.URL.Query().Get("cursor"),
	)
	if err != nil {
		h.writeError(w, err)
		return
	}
	items := make([]Response, 0, len(users))
	for _, u := range users {
		items = append(items, ToResponse(u))
	}
	httpx.WriteJSON(w, http.StatusOK, page{Items: items, NextCursor: next})
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
	// Who granted it. The column stays nullable because rows written before there
	// was an authenticated caller have nothing to put there, but every new one
	// records somebody.
	caller, err := authz.FromContext(r.Context())
	if err != nil {
		// Only reachable if this route were mounted without its guard, which would
		// be a wiring mistake rather than a caller's — so it fails loudly here
		// instead of recording an anonymous grant.
		h.writeError(w, err)
		return
	}
	if err := h.svc.Grant(ctx, id, Role(r.PathValue("role")), &caller.Subject); err != nil {
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
	case errors.Is(err, ErrGranterGone):
		// 401 rather than 404: what is missing is the caller, not the target, and
		// the thing to do about it is sign in again as somebody who exists.
		httpx.WriteError(w, http.StatusUnauthorized, err.Error())
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "user not found")
	case errors.Is(err, ErrLastAdmin):
		httpx.WriteError(w, http.StatusConflict,
			"this is the last administrator, and removing them would leave nobody able to "+
				"administer the platform")
	case errors.Is(err, ErrNotProvisioned):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrConflict):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("user handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
