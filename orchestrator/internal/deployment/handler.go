package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// defaultLogTail bounds the history replayed when a pod-log stream connects, so a
// long-running pod doesn't dump its whole buffer before tailing begins.
const defaultLogTail = 500

// requestTimeout bounds the database + Kubernetes work behind a single request.
// It is more generous than the integration handler's since a deploy touches the
// cluster as well as the database.
const requestTimeout = 15 * time.Second

// Handler serves the deployment REST endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc. Live status updates are no longer
// streamed from here: the informer callback publishes snapshots to NATS and the
// BFF serves the SSE (issue #74); clients poll the list as a fallback.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the deployment routes to mux. Deploy/list are nested under
// an integration; get/undeploy address a deployment directly.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations/{id}/deployments", h.deploy)
	mux.HandleFunc("GET /integrations/{id}/deployments", h.listByIntegration)
	mux.HandleFunc("GET /integrations/{id}/deployments/options", h.deployOptions)
	mux.HandleFunc("GET /deployments/{id}", h.get)
	mux.HandleFunc("GET /deployments/{id}/pods/{pod}/logs", h.podLogs)
	mux.HandleFunc("PATCH /deployments/{id}", h.scale)
	mux.HandleFunc("POST /deployments/{id}/rollout", h.rollout)
	mux.HandleFunc("DELETE /deployments/{id}", h.undeploy)
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
	Tag             string        `json:"tag,omitempty"`
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
		Tag:             meta.Tag,
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
	Networked       bool             `json:"networked"`
	SuggestedSlug   string           `json:"suggestedSlug,omitempty"`
	EnvVars         []envVarResponse `json:"envVars,omitempty"`
	EnvProvidedKeys []string         `json:"envProvidedKeys,omitempty"`
	Slug            string           `json:"slug,omitempty"`
	SlugValid       bool             `json:"slugValid"`
	SlugAvailable   bool             `json:"slugAvailable"`
}

// envVarResponse is the wire form of one declared environment variable the modal
// prompts the operator to fill.
type envVarResponse struct {
	Name     string `json:"name"`
	Default  string `json:"default,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// deployOptions backs the deploy modal: with no slug query it reports whether the
// integration is networked and a free slug to suggest; with ?slug= it validates
// that candidate (?expose=external also checks the subdomain is free).
func (h *Handler) deployOptions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	external := r.URL.Query().Get("expose") == ExposeExternal
	snapshotID := r.URL.Query().Get("snapshotId")
	opts, err := h.svc.DeployOptions(ctx, r.PathValue("id"), r.URL.Query().Get("slug"), external, snapshotID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	envVars := make([]envVarResponse, 0, len(opts.EnvVars))
	for _, e := range opts.EnvVars {
		envVars = append(envVars, envVarResponse(e))
	}
	httpx.WriteJSON(w, http.StatusOK, deployOptionsResponse{
		Networked:       opts.Networked,
		SuggestedSlug:   opts.SuggestedSlug,
		EnvVars:         envVars,
		EnvProvidedKeys: opts.EnvProvidedKeys,
		Slug:            opts.Slug,
		SlugValid:       opts.SlugValid,
		SlugAvailable:   opts.SlugAvailable,
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

// podLogs streams a pod's container logs as plain text (chunked). With ?follow=1
// it tails the pod live, holding the connection open until the client disconnects
// (which cancels the request context and closes the k8s stream). It bypasses the
// shared request timeout since a follow stream is long-lived by design; the tail
// is bounded so the initial replay stays small.
func (h *Handler) podLogs(w http.ResponseWriter, r *http.Request) {
	follow := r.URL.Query().Get("follow") == "1" || r.URL.Query().Get("follow") == "true"
	tail := int64(defaultLogTail)
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			tail = n
		}
	}

	stream, err := h.svc.PodLogs(r.Context(), r.PathValue("id"), r.PathValue("pod"), follow, tail)
	if err != nil {
		h.writeError(w, err)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("X-Accel-Buffering", "no") // don't let a proxy buffer the tail
	w.WriteHeader(http.StatusOK)

	// Copy through, flushing each chunk so lines reach the client as they arrive
	// rather than being held in a buffer. A dead client cancels the context, which
	// ends stream.Read; io.Copy with a flushing writer keeps it simple.
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, rerr := stream.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if rerr != io.EOF && r.Context().Err() == nil {
				slog.Error("pod log stream", "error", rerr)
			}
			return
		}
	}
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

// rolloutRequest is the body of a rollout request: the version tag (snapshot id)
// to upgrade the live deployment to.
type rolloutRequest struct {
	SnapshotID string `json:"snapshotId"`
}

func (h *Handler) rollout(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	var req rolloutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	d, err := h.svc.Rollout(ctx, r.PathValue("id"), req.SnapshotID)
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
	case errors.Is(err, ErrPodNotFound):
		httpx.WriteError(w, http.StatusNotFound, "pod not found")
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
	case errors.Is(err, ErrSecretNotFound):
		httpx.WriteError(w, http.StatusBadRequest, "a referenced secret does not exist")
	case errors.Is(err, ErrReservedEnvVar):
		httpx.WriteError(w, http.StatusBadRequest, "HTTP_PORT and HTTP_HOST are managed by the orchestrator")
	case errors.Is(err, ErrMissingRequiredEnv):
		// Surface the full message (it names the missing variables) so the deploy
		// UI can list exactly which keys must be provided.
		httpx.WriteError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, ErrSnapshotRequired):
		httpx.WriteError(w, http.StatusBadRequest, "a version tag is required to deploy")
	case errors.Is(err, ErrSnapshotNotFound):
		httpx.WriteError(w, http.StatusBadRequest, "the selected version tag was not found")
	case errors.Is(err, ErrSnapshotMismatch):
		httpx.WriteError(w, http.StatusBadRequest, "the selected version tag does not belong to this integration")
	case errors.Is(err, ErrRolloutTopologyChange):
		httpx.WriteError(w, http.StatusBadRequest, "that version changes the HTTP source; undeploy and redeploy instead")
	default:
		slog.Error("deployment handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
