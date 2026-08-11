package agent

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	agentapp "github.com/juancavallotti/octo/orchestrator/agent"
	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/sitesettings"
)

// requestTimeout bounds one request. Longer than the settings handlers' five
// seconds because an install is not a settings write: it touches the database, the
// cluster's Secret, and a deploy that waits on the API server.
const requestTimeout = 60 * time.Second

// Handler serves the agent routes.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /settings/agent", h.get)
	mux.HandleFunc("POST /settings/agent/install", h.install)
	mux.HandleFunc("POST /settings/agent/rollout", h.rollout)
	mux.HandleFunc("POST /settings/agent/tracing", h.tracing)
	mux.HandleFunc("DELETE /settings/agent", h.uninstall)
}

// statusResponse is the wire representation. It carries no definition and no key
// material — only what decides which button an operator is offered.
type statusResponse struct {
	State           string `json:"state"`
	IntegrationID   string `json:"integrationId,omitempty"`
	DeploymentID    string `json:"deploymentId,omitempty"`
	InternalURL     string `json:"internalUrl,omitempty"`
	InstalledTag    string `json:"installedTag,omitempty"`
	UpdateAvailable bool   `json:"updateAvailable"`
	Edited          bool   `json:"edited"`
	Tracing         bool   `json:"tracing"`
	// Blocked is empty, or names what stands in the way: kubernetes, encryption or
	// llm_key.
	Blocked          string `json:"blocked,omitempty"`
	DeploymentStatus string `json:"deploymentStatus,omitempty"`
	Reason           string `json:"reason,omitempty"`
}

// actorRequest carries the acting user's id, as every write route in this service
// does: the orchestrator has no session and trusts the BFF as the auth boundary.
type actorRequest struct {
	ActorID string `json:"actorId"`
}

// tracingRequest is the body of the tracing toggle.
type tracingRequest struct {
	ActorID string `json:"actorId"`
	Tracing bool   `json:"tracing"`
}

func toResponse(s Status) statusResponse {
	return statusResponse{
		State:            s.State,
		IntegrationID:    s.IntegrationID,
		DeploymentID:     s.DeploymentID,
		InternalURL:      s.InternalURL,
		InstalledTag:     s.InstalledTag,
		UpdateAvailable:  s.UpdateAvailable,
		Edited:           s.Edited,
		Tracing:          s.Tracing,
		Blocked:          s.Blocked,
		DeploymentStatus: s.DeploymentStatus,
		Reason:           s.Reason,
	}
}

// get godoc
//
//	@Summary		The platform agent's status
//	@Description	What is installed, what is running, and what stands in the way. Never
//	@Description	fails for a reason the caller could act on: no cluster access, no
//	@Description	encryption key and no stored LLM key are reported in `blocked` rather
//	@Description	than as errors, because a page that 500s cannot say what to configure.
//	@Tags			agent
//	@Produce		json
//	@Success		200	{object}	statusResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/settings/agent [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	status, err := h.svc.Status(ctx)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(status))
}

// install godoc
//
//	@Summary		Install the platform agent
//	@Description	Creates the agent as an ordinary integration, writes its skills as
//	@Description	resources, tags a version from the bundle this binary ships, and deploys
//	@Description	it internal-only. Idempotent: an existing integration is reused and an
//	@Description	existing tag is not recreated, so installing twice is not an error and
//	@Description	does not produce a second agent.
//	@Tags			agent
//	@Accept			json
//	@Produce		json
//	@Param			body	body		actorRequest	false	"The acting user"
//	@Success		200		{object}	statusResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"no llm key is configured, or the name is taken"
//	@Failure		503		{object}	httpx.ErrorResponse	"no cluster access, or encryption is not configured"
//	@Router			/settings/agent/install [post]
func (h *Handler) install(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, func(ctx context.Context, actorID string) (Status, error) {
		return h.svc.Install(ctx, actorID)
	})
}

// rollout godoc
//
//	@Summary		Roll the agent onto the bundle this binary ships
//	@Description	Publishes a new version tag and rolls the deployment onto it, also
//	@Description	re-resolving the provider and model from the site's LLM settings.
//	@Description	Tagging freezes the integration's working copy, so this replaces any
//	@Description	local edits with the shipped agent — the status reports `edited` so the
//	@Description	choice can be made knowingly.
//	@Tags			agent
//	@Accept			json
//	@Produce		json
//	@Param			body	body		actorRequest	false	"The acting user"
//	@Success		200		{object}	statusResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"not installed, or no llm key is configured"
//	@Failure		503		{object}	httpx.ErrorResponse	"no cluster access"
//	@Router			/settings/agent/rollout [post]
func (h *Handler) rollout(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, func(ctx context.Context, actorID string) (Status, error) {
		return h.svc.Rollout(ctx, actorID)
	})
}

// tracing godoc
//
//	@Summary		Turn tracing on or off for the agent
//	@Description	A rolling update, because the runtime reads the setting at startup — so
//	@Description	it only reaches the agent by replacing its pods. Nothing else changes:
//	@Description	the same version tag and the same environment.
//	@Tags			agent
//	@Accept			json
//	@Produce		json
//	@Param			body	body		tracingRequest	true	"Whether tracing is on"
//	@Success		200		{object}	statusResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"not installed, or not deployed"
//	@Failure		503		{object}	httpx.ErrorResponse	"no cluster access"
//	@Router			/settings/agent/tracing [post]
func (h *Handler) tracing(w http.ResponseWriter, r *http.Request) {
	var req tracingRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	status, err := h.svc.SetTracing(ctx, req.Tracing)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(status))
}

// uninstall godoc
//
//	@Summary		Remove the agent
//	@Description	Undeploys it. The integration is kept by default because it holds the
//	@Description	definition, which may have been edited; purge deletes that too.
//	@Tags			agent
//	@Produce		json
//	@Param			purge	query	boolean	false	"Also delete the integration"
//	@Success		204		"removed"
//	@Failure		409		{object}	httpx.ErrorResponse	"not installed"
//	@Failure		503		{object}	httpx.ErrorResponse	"no cluster access"
//	@Router			/settings/agent [delete]
func (h *Handler) uninstall(w http.ResponseWriter, r *http.Request) {
	purge := r.URL.Query().Get("purge") == "1" || r.URL.Query().Get("purge") == "true"

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Uninstall(ctx, purge); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// act runs one of the two write operations that take only an actor, decoding an
// optional body — an empty one is fine, since the actor is advisory attribution
// rather than authorisation.
func (h *Handler) act(
	w http.ResponseWriter, r *http.Request,
	run func(ctx context.Context, actorID string) (Status, error),
) {
	var req actorRequest
	if r.ContentLength > 0 {
		if err := httpx.DecodeJSON(w, r, &req); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	status, err := run(ctx, req.ActorID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(status))
}

// writeError maps domain errors to statuses. These strings reach the operator
// verbatim — the BFF passes the error envelope straight through — so each says what
// to do rather than what went wrong.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrClusterUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"the agent needs in-cluster access, which this orchestrator does not have")
	case errors.Is(err, sitesettings.ErrEncryptionUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"cannot read the llm api key: encryption is not configured (KV_ENCRYPTION_KEY)")
	case errors.Is(err, sitesettings.ErrNotConfigured):
		httpx.WriteError(w, http.StatusConflict,
			"no llm api key is configured — set one under Admin, LLM provider")
	case errors.Is(err, ErrNoOrchestratorURL):
		httpx.WriteError(w, http.StatusServiceUnavailable,
			"the agent needs ORCHESTRATOR_URL set on the orchestrator to call back to it")
	case errors.Is(err, ErrUnknownProvider):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNotInstalled):
		httpx.WriteError(w, http.StatusConflict, "the agent is not installed")
	case errors.Is(err, ErrNotDeployed):
		httpx.WriteError(w, http.StatusConflict, "the agent is not deployed")
	case errors.Is(err, integration.ErrNameTaken):
		httpx.WriteError(w, http.StatusConflict,
			"an integration named \""+agentapp.Name+"\" already exists — rename it, then install")
	default:
		slog.Error("agent handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
