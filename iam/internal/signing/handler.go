package signing

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	httpx "github.com/juancavallotti/octo/iam/internal/http"
)

const (
	// requestTimeout bounds the database work behind a single request.
	requestTimeout = 5 * time.Second

	// jwksPath and discoveryPath are the well-known locations. The discovery
	// document is served alongside the keys so that a verifier given only the issuer
	// can find them.
	jwksPath      = "/.well-known/jwks.json"
	discoveryPath = "/.well-known/openid-configuration"

	// jwksMaxAge is how long a caller may cache the key set. Well under the grace
	// period a retired key stays published for, so a client holding a stale copy
	// still holds one that verifies.
	jwksMaxAge = 5 * time.Minute
)

// Handler serves the keys and the discovery document.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the well-known routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+jwksPath, h.jwks)
	mux.HandleFunc("GET "+discoveryPath, h.discovery)
}

// discoveryDocument is the subset of OpenID Provider Metadata that is true of this
// service. Not a full one: iam runs no authorization flow — it signs tokens for
// principals another provider already authenticated — so there is no
// authorization_endpoint to advertise.
type discoveryDocument struct {
	Issuer                           string   `json:"issuer"`
	JWKSURI                          string   `json:"jwks_uri"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	ClaimsSupported                  []string `json:"claims_supported"`
}

func (h *Handler) discovery(w http.ResponseWriter, _ *http.Request) {
	issuer := h.svc.Issuer()
	httpx.WriteJSON(w, http.StatusOK, discoveryDocument{
		Issuer:                           issuer,
		JWKSURI:                          issuer + jwksPath,
		IDTokenSigningAlgValuesSupported: []string{signingAlgorithm},
		SubjectTypesSupported:            []string{"public"},
		// What a verifier will actually find in a platform token, beyond the
		// registered claims every JWT carries.
		ClaimsSupported: []string{
			"iss", "sub", "aud", "iat", "nbf", "exp", "jti",
			"email", "name", "roles",
		},
	})
}

func (h *Handler) jwks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	set, err := h.svc.JWKS(ctx)
	if err != nil {
		slog.Error("jwks handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Cacheable: a caller fetches the keys once and then checks tokens on its own.
	w.Header().Set("Cache-Control", "public, max-age="+jwksMaxAgeSeconds)
	httpx.WriteJSON(w, http.StatusOK, set)
}

// jwksMaxAgeSeconds is jwksMaxAge rendered for the Cache-Control header, computed
// once rather than formatted per request.
var jwksMaxAgeSeconds = strconv.Itoa(int(jwksMaxAge.Seconds()))
