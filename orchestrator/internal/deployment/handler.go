package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/eip-go/orchestrator/internal/http"
)

// requestTimeout bounds the database + Kubernetes work behind a single request.
// It is more generous than the integration handler's since a deploy touches the
// cluster as well as the database.
const requestTimeout = 15 * time.Second

// eventsKeepAlive is how often the SSE stream sends a comment so proxies keep the
// connection open during quiet periods.
const eventsKeepAlive = 15 * time.Second

// Handler serves the deployment REST endpoints.
type Handler struct {
	svc *Service
	hub *Hub // change notifications for the SSE stream; nil disables /events
}

// NewHandler returns a Handler backed by svc. hub may be nil, in which case the
// SSE events route is not registered (clients fall back to polling the list).
func NewHandler(svc *Service, hub *Hub) *Handler {
	return &Handler{svc: svc, hub: hub}
}

// Register attaches the deployment routes to mux. Deploy/list are nested under
// an integration; get/undeploy address a deployment directly.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations/{id}/deployments", h.deploy)
	mux.HandleFunc("GET /integrations/{id}/deployments", h.listByIntegration)
	mux.HandleFunc("GET /integrations/{id}/deployments/options", h.deployOptions)
	mux.HandleFunc("GET /deployments/{id}", h.get)
	mux.HandleFunc("PATCH /deployments/{id}", h.scale)
	mux.HandleFunc("DELETE /deployments/{id}", h.undeploy)
	if h.hub != nil {
		mux.HandleFunc("GET /integrations/{id}/deployments/events", h.events)
	}
}

// podResponse is the wire representation of one runtime pod.
type podResponse struct {
	Name     string `json:"name"`
	Phase    string `json:"phase"`
	Ready    bool   `json:"ready"`
	Restarts int32  `json:"restarts"`
}

// deploymentResponse is the wire representation of a deployment. The display
// name, replica count and URLs are lifted out of the jsonb columns; the replica
// counts, pods, reason and createdAt come from the live cluster status.
type deploymentResponse struct {
	ID              string        `json:"id"`
	IntegrationID   string        `json:"integrationId"`
	Name            string        `json:"name"`
	Status          string        `json:"status"`
	Replicas        int           `json:"replicas"`
	ReadyReplicas   int32         `json:"readyReplicas"`
	DesiredReplicas int32         `json:"desiredReplicas"`
	Reason          string        `json:"reason,omitempty"`
	Pods            []podResponse `json:"pods,omitempty"`
	InternalURL     string        `json:"internalUrl,omitempty"`
	ExternalURL     string        `json:"externalUrl,omitempty"`
	CreatedAt       *time.Time    `json:"createdAt,omitempty"`
	LastUpdated     time.Time     `json:"lastUpdated"`
}

func toResponse(d Deployment) deploymentResponse {
	meta := ParseMetadata(d.Metadata)
	replicas := ParseSettings(d.Settings).Replicas
	if replicas < 1 {
		replicas = 1
	}
	resp := deploymentResponse{
		ID:              d.ID,
		IntegrationID:   d.IntegrationID,
		Name:            meta.Name,
		Status:          d.Status,
		Replicas:        replicas,
		ReadyReplicas:   d.Detail.ReadyReplicas,
		DesiredReplicas: d.Detail.DesiredReplicas,
		Reason:          d.Detail.Reason,
		InternalURL:     meta.InternalURL,
		ExternalURL:     meta.ExternalURL,
		LastUpdated:     d.LastUpdated,
	}
	if !d.Detail.CreatedAt.IsZero() {
		t := d.Detail.CreatedAt
		resp.CreatedAt = &t
	}
	for _, p := range d.Detail.Pods {
		resp.Pods = append(resp.Pods, podResponse{Name: p.Name, Phase: p.Phase, Ready: p.Ready, Restarts: p.Restarts})
	}
	return resp
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

// deployOptionsResponse is the wire form of the deploy choices for an integration.
// The slug* fields are populated only when the request carried a candidate slug.
type deployOptionsResponse struct {
	Networked     bool   `json:"networked"`
	SuggestedSlug string `json:"suggestedSlug,omitempty"`
	Slug          string `json:"slug,omitempty"`
	SlugValid     bool   `json:"slugValid"`
	SlugAvailable bool   `json:"slugAvailable"`
}

// deployOptions backs the deploy modal: with no slug query it reports whether the
// integration is networked and a free slug to suggest; with ?slug= it validates
// that candidate (?expose=external also checks the subdomain is free).
func (h *Handler) deployOptions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	external := r.URL.Query().Get("expose") == ExposeExternal
	opts, err := h.svc.DeployOptions(ctx, r.PathValue("id"), r.URL.Query().Get("slug"), external)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, deployOptionsResponse{
		Networked:     opts.Networked,
		SuggestedSlug: opts.SuggestedSlug,
		Slug:          opts.Slug,
		SlugValid:     opts.SlugValid,
		SlugAvailable: opts.SlugAvailable,
	})
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

// events streams the integration's deployment list over Server-Sent Events: an
// initial snapshot on connect, then a fresh snapshot whenever the cluster reports
// a change (via the hub), with periodic keep-alive comments. It returns when the
// client disconnects.
func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpx.WriteError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	id := r.PathValue("id")

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ticks, cancel := h.hub.Subscribe(id)
	defer cancel()

	ctx := r.Context()
	if !h.writeSnapshot(ctx, w, flusher, id) {
		return
	}

	keepAlive := time.NewTicker(eventsKeepAlive)
	defer keepAlive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if !h.writeSnapshot(ctx, w, flusher, id) {
				return
			}
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// writeSnapshot sends the current deployment list as one SSE data event. It
// reports whether the stream is still healthy; a transient read error keeps the
// stream open (returns true) while a write error ends it (returns false).
func (h *Handler) writeSnapshot(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, integrationID string) bool {
	sctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	items, err := h.svc.ListByIntegration(sctx, integrationID)
	if err != nil {
		slog.Error("deployment events snapshot", "integrationId", integrationID, "error", err)
		return ctx.Err() == nil // keep the stream open unless the client is gone
	}
	out := make([]deploymentResponse, 0, len(items))
	for _, d := range items {
		out = append(out, toResponse(d))
	}
	data, err := json.Marshal(out)
	if err != nil {
		slog.Error("deployment events marshal", "integrationId", integrationID, "error", err)
		return true
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return false
	}
	flusher.Flush()
	return true
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

// scaleRequest is the body of a scale request: the new desired replica count.
type scaleRequest struct {
	Replicas int `json:"replicas"`
}

func (h *Handler) scale(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	var req scaleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	d, err := h.svc.Scale(ctx, r.PathValue("id"), req.Replicas)
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
	case errors.Is(err, ErrInvalidSlug):
		httpx.WriteError(w, http.StatusBadRequest, "invalid deployment slug")
	case errors.Is(err, ErrSlugTaken):
		httpx.WriteError(w, http.StatusConflict, "deployment slug already in use")
	case errors.Is(err, ErrSubdomainTaken):
		httpx.WriteError(w, http.StatusConflict, "external subdomain already in use by another integration")
	default:
		slog.Error("deployment handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
