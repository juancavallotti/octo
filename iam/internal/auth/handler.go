package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	httpx "github.com/juancavallotti/octo/iam/internal/http"
	"github.com/juancavallotti/octo/iam/internal/user"
)

const (
	// requestTimeout bounds one exchange. More generous than the read routes'
	// because it may include discovering the identity provider, which is a request
	// to somebody else's server.
	requestTimeout = 15 * time.Second

	authHeader = "Authorization"
	// bearerPrefix is stripped case-insensitively, since RFC 6750 makes the scheme
	// name case-insensitive and clients disagree about how to spell it.
	bearerPrefix = "bearer "
)

// Handler serves the token exchange.
//
// svc may be nil, and that is a supported state rather than an oversight: an
// install with no identity provider configured still registers this route, and it
// answers 503 naming what is missing. A route that vanished with its dependency
// would leave a caller unable to tell a misconfigured install from a build that
// never had the feature — the same reasoning the orchestrator's agent status
// route already follows.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc, which may be nil.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the exchange and the refresh to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /auth", h.exchange)
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/machine", h.machine)
}

// response is what a successful exchange returns: the token, when it stops being
// valid, and who it speaks for. The expiry is given explicitly so a client can
// schedule its refresh without decoding the token it was handed.
type response struct {
	Token     string        `json:"token"`
	ExpiresAt time.Time     `json:"expiresAt"`
	User      user.Response `json:"user"`
}

func (h *Handler) exchange(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"no identity provider is configured; set OIDC_ISSUER and OIDC_CLIENT_ID")
		return
	}

	token := bearerToken(r)
	if token == "" {
		// The challenge tells a client what this endpoint wants, which is the one
		// thing worth saying to a caller who has not authenticated.
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteError(w, http.StatusUnauthorized, "a bearer token is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	result, err := h.svc.Exchange(ctx, token)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeToken(w, result)
}

// refresh trades a platform token for a fresh one. Deliberately the same shape as
// the exchange — a bearer, no body, the same reply — so a client has one thing to
// call and one thing to parse, differing only in which token it presents.
func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"no identity provider is configured; set OIDC_ISSUER and OIDC_CLIENT_ID")
		return
	}

	token := bearerToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteError(w, http.StatusUnauthorized, "a bearer token is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	result, err := h.svc.Refresh(ctx, token)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeToken(w, result)
}

// machineRequest names the deployed integration a token is being minted for.
type machineRequest struct {
	Deployment string `json:"deployment"`
}

// machine issues a token for a deployed integration, on the authority of the
// person deploying it — whose own platform token is the bearer here.
//
// Unlike the other two this takes a body, because the deployment is not something
// the credential can say. It is the caller's assertion about what they are
// deploying, which is safe: the token it produces can only ever act as them and
// only ever with the runtime role, so naming a different deployment buys nothing
// that naming their own would not.
func (h *Handler) machine(w http.ResponseWriter, r *http.Request) {
	if h.svc == nil {
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"no identity provider is configured; set OIDC_ISSUER and OIDC_CLIENT_ID")
		return
	}

	token := bearerToken(r)
	if token == "" {
		w.Header().Set("WWW-Authenticate", "Bearer")
		httpx.WriteError(w, http.StatusUnauthorized, "a bearer token is required")
		return
	}

	var req machineRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "the request body is not valid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	result, err := h.svc.MintMachine(ctx, token, req.Deployment)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeToken(w, result)
}

// writeToken is the reply both endpoints send.
func (h *Handler) writeToken(w http.ResponseWriter, result Result) {
	// Never cached, anywhere. It is a credential.
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, response{
		Token:     result.Token.Value,
		ExpiresAt: result.Token.ExpiresAt,
		User:      user.ToResponse(result.User),
	})
}

// bearerToken reads the presented credential out of the Authorization header,
// returning "" when there is none to read.
func bearerToken(r *http.Request) string {
	value := r.Header.Get(authHeader)
	if len(value) < len(bearerPrefix) ||
		!strings.EqualFold(value[:len(bearerPrefix)], bearerPrefix) {
		return ""
	}
	return strings.TrimSpace(value[len(bearerPrefix):])
}

// writeError maps the exchange's failures to statuses.
//
// The distinction that matters is between the caller's problem and ours. A token
// that does not verify is 401 and says only that. A provider we could not reach
// is 503: the caller's token may be perfectly good, and telling them it was
// rejected would send them to re-authenticate against a provider that is down.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrUnauthenticated):
		// Logged in full, answered in one word. The reason a token failed is a hint
		// to whoever is guessing at one.
		slog.Info("auth exchange rejected a token", "error", err)
		w.Header().Set("WWW-Authenticate", "Bearer error=\"invalid_token\"")
		httpx.WriteError(w, http.StatusUnauthorized, "the token is not valid")
	case errors.Is(err, ErrUnavailable):
		// 503 and not 401, so a platform holding a valid credential keeps it rather
		// than signing its users out over a fault on this side.
		slog.Error("auth could not answer", "error", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "this service cannot answer right now")
	case errors.Is(err, ErrProviderUnreachable):
		slog.Error("auth exchange could not reach the identity provider", "error", err)
		httpx.WriteError(w, http.StatusServiceUnavailable, "the identity provider is unreachable")
	case errors.Is(err, ErrForbidden):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, user.ErrNotProvisioned):
		// 403 and not 401: they authenticated perfectly well. Re-authenticating
		// would change nothing, and telling them to try again would be a lie. What
		// has to happen is that somebody gives them an account.
		slog.Info("a verified caller has no account on this platform", "error", err)
		httpx.WriteError(w, http.StatusForbidden,
			"this account has not been provisioned on this platform; ask an administrator to add you")
	case errors.Is(err, user.ErrInvalid):
		// The provider verified a token describing a principal we cannot store —
		// no subject, or no email. The caller cannot fix it and neither can we, so
		// it says what is wrong with the token rather than pretending it is invalid.
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	default:
		slog.Error("auth handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
