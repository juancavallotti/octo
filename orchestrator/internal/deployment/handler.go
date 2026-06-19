package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/eip-go/orchestrator/internal/http"
)

// requestTimeout bounds the database + Kubernetes work behind a single request.
// It is more generous than the integration handler's since a deploy touches the
// cluster as well as the database.
const requestTimeout = 15 * time.Second

// Handler serves the deployment REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the deployment routes to mux. Deploy/list are nested under
// an integration; get/undeploy address a deployment directly.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations/{id}/deployments", h.deploy)
	mux.HandleFunc("GET /integrations/{id}/deployments", h.listByIntegration)
	mux.HandleFunc("GET /deployments/{id}", h.get)
	mux.HandleFunc("DELETE /deployments/{id}", h.undeploy)
}

// deploymentResponse is the wire representation of a deployment. The display
// name, replica count and internal URL are lifted out of the jsonb columns for
// convenience.
type deploymentResponse struct {
	ID            string    `json:"id"`
	IntegrationID string    `json:"integrationId"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	Replicas      int       `json:"replicas"`
	InternalURL   string    `json:"internalUrl,omitempty"`
	ExternalURL   string    `json:"externalUrl,omitempty"`
	LastUpdated   time.Time `json:"lastUpdated"`
}

func toResponse(d Deployment) deploymentResponse {
	meta := ParseMetadata(d.Metadata)
	replicas := ParseSettings(d.Settings).Replicas
	if replicas < 1 {
		replicas = 1
	}
	return deploymentResponse{
		ID:            d.ID,
		IntegrationID: d.IntegrationID,
		Name:          meta.Name,
		Status:        d.Status,
		Replicas:      replicas,
		InternalURL:   meta.InternalURL,
		ExternalURL:   meta.ExternalURL,
		LastUpdated:   d.LastUpdated,
	}
}

func (h *Handler) deploy(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	// The settings body is optional; ignore an empty/malformed body and fall
	// back to defaults (single replica).
	var settings Settings
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&settings)
	}

	d, err := h.svc.Deploy(ctx, r.PathValue("id"), settings)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(d))
}

func (h *Handler) listByIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.ListByIntegration(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]deploymentResponse, 0, len(items))
	for _, d := range items {
		out = append(out, toResponse(d))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	d, err := h.svc.Get(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(d))
}

func (h *Handler) undeploy(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Undeploy(ctx, r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are
// logged and reported generically so internals do not leak to clients.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "deployment not found")
	case errors.Is(err, ErrIntegrationNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	case errors.Is(err, ErrUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, "deployments are not available")
	case errors.Is(err, ErrExternalUnavailable):
		httpx.WriteError(w, http.StatusBadRequest, "external endpoints are not configured")
	case errors.Is(err, ErrInvalidSubdomain):
		httpx.WriteError(w, http.StatusBadRequest, "invalid external subdomain")
	default:
		slog.Error("deployment handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
