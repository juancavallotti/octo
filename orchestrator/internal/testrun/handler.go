package testrun

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/authz"
	"github.com/juancavallotti/octo/orchestrator/internal/deployment"
	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
	"github.com/juancavallotti/octo/orchestrator/internal/kube"
)

// requestTimeout bounds the database and Kubernetes work behind one request.
const requestTimeout = 15 * time.Second

// Handler serves the test run REST endpoints, nested under a deployment.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the test run routes to mux.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /deployments/{id}/testruns", h.start)
	mux.HandleFunc("GET /deployments/{id}/testruns", h.list)
	mux.HandleFunc("GET /deployments/{id}/testruns/{runId}", h.get)
	mux.HandleFunc("GET /upgrades", h.upgrades)
}

// runResponse is the wire representation of a test run.
type runResponse struct {
	ID           string `json:"id"`
	DeploymentID string `json:"deploymentId"`
	SnapshotID   string `json:"snapshotId"`
	// RuntimeVersion is the runtime release the suites ran against.
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
	// Status is running, passed, failed, errored or timed_out.
	Status        string     `json:"status"`
	RolloutOnPass bool       `json:"rolloutOnPass"`
	RolledOutAt   *time.Time `json:"rolledOutAt,omitempty"`
	// Report is dolphin's JSON report: totals, and every suite and case.
	Report map[string]any `json:"report,omitempty"`
	// Error says why a run errored, or why a rollout it asked for did not happen.
	Error      string     `json:"error,omitempty"`
	CreatedBy  string     `json:"createdBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// toResponse builds the wire form of a run.
func toResponse(r Run) runResponse {
	ret := runResponse{
		ID:             r.ID,
		DeploymentID:   r.DeploymentID,
		SnapshotID:     r.SnapshotID,
		RuntimeVersion: r.RuntimeVersion,
		Status:         string(r.Status),
		RolloutOnPass:  r.RolloutOnPass,
		RolledOutAt:    r.RolledOutAt,
		Error:          r.Error,
		CreatedBy:      r.CreatedBy,
		CreatedAt:      r.CreatedAt,
		FinishedAt:     r.FinishedAt,
	}
	if len(r.Report) > 0 {
		if err := json.Unmarshal(r.Report, &ret.Report); err != nil {
			slog.Error("decode a stored test report", "run", r.ID, "error", err)
		}
	}
	return ret
}

// startRequest is the body of a test run request.
type startRequest struct {
	// SnapshotID is the version to test; empty tests the one the deployment runs.
	SnapshotID string `json:"snapshotId"`
	// RolloutOnPass rolls the deployment onto that version when every case passes,
	// keeping its env and tracing.
	RolloutOnPass bool `json:"rolloutOnPass"`
}

// start godoc
//
//	@Summary		Run a deployment's test suites
//	@Description	Runs the suites frozen under a version of the deployment's integration, with the
//	@Description	deployment's env, against the runtime release this install deploys. The run happens in
//	@Description	a Job of its own; poll it for the verdict. With rolloutOnPass, a run in which every case
//	@Description	passes rolls the deployment onto the tested version.
//	@Tags			deployments
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Deployment id"
//	@Param			body	body		startRequest	true	"Version to test, and whether to roll out on pass"
//	@Success		202		{object}	runResponse
//	@Failure		400		{object}	httpx.ErrorResponse
//	@Failure		404		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"the test image is another release than the deployed runtime"
//	@Failure		422		{object}	httpx.ErrorResponse	"the version has no test suites, or rolloutOnPass on a pinned deployment"
//	@Failure		503		{object}	httpx.ErrorResponse	"this install cannot run tests"
//	@Router			/deployments/{id}/testruns [post]
func (h *Handler) start(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	var req startRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	createdBy := ""
	if p, ok := authz.FromContext(ctx); ok {
		createdBy = p.Subject
	}
	run, err := h.svc.Start(ctx, r.PathValue("id"), req.SnapshotID, req.RolloutOnPass, createdBy)
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, toResponse(run))
}

// list godoc
//
//	@Summary	List a deployment's test runs
//	@Description	The most recent runs, newest first.
//	@Tags		deployments
//	@Produce	json
//	@Param		id	path		string	true	"Deployment id"
//	@Success	200	{array}		runResponse
//	@Failure	500	{object}	httpx.ErrorResponse
//	@Router		/deployments/{id}/testruns [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	runs, err := h.svc.List(ctx, r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	out := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		out = append(out, toResponse(run))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// get godoc
//
//	@Summary	Get a test run
//	@Tags		deployments
//	@Produce	json
//	@Param		id		path		string	true	"Deployment id"
//	@Param		runId	path		string	true	"Test run id"
//	@Success	200		{object}	runResponse
//	@Failure	404		{object}	httpx.ErrorResponse
//	@Router		/deployments/{id}/testruns/{runId} [get]
func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	run, err := h.svc.Get(ctx, r.PathValue("id"), r.PathValue("runId"))
	if err != nil {
		writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(run))
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are
// logged and reported generically so internals do not leak to clients.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "test run not found")
	case errors.Is(err, deployment.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "deployment not found")
	case errors.Is(err, ErrUnavailable), errors.Is(err, deployment.ErrUnavailable):
		httpx.WriteError(w, http.StatusServiceUnavailable, "test runs are not available on this installation")
	case errors.Is(err, ErrRuntimeMismatch):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrPinned):
		httpx.WriteError(w, http.StatusUnprocessableEntity,
			"the deployment is pinned to a runtime release; change its runtime to upgrade it")
	case errors.Is(err, ErrNoSuites):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "this version of the integration has no test suites")
	case errors.Is(err, kube.ErrTestBundleTooLarge):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "the integration is too large to test")
	case errors.Is(err, deployment.ErrSnapshotRequired):
		httpx.WriteError(w, http.StatusBadRequest, "the deployment runs no version tag to test")
	case errors.Is(err, deployment.ErrSnapshotNotFound):
		httpx.WriteError(w, http.StatusBadRequest, "the selected version tag was not found")
	case errors.Is(err, deployment.ErrSnapshotMismatch):
		httpx.WriteError(w, http.StatusBadRequest, "the selected version tag does not belong to this integration")
	case errors.Is(err, deployment.ErrSecretNotFound):
		httpx.WriteError(w, http.StatusBadRequest, "a referenced secret does not exist")
	default:
		slog.Error("test run handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

// upgradeResponse is one deployment as an upgrade sees it.
type upgradeResponse struct {
	DeploymentID  string `json:"deploymentId"`
	IntegrationID string `json:"integrationId"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Tag           string `json:"tag,omitempty"`
	SnapshotID    string `json:"snapshotId,omitempty"`
	// RuntimeVersion is the runtime release the deployment's pods run.
	RuntimeVersion string `json:"runtimeVersion,omitempty"`
	// UpgradeTo is the newer release a rollout would move it onto; absent when
	// it is current.
	UpgradeTo string `json:"upgradeTo,omitempty"`
	// HasSuites says whether the version it runs has test suites to run.
	HasSuites bool `json:"hasSuites"`
	// Pinned says the deployment is pinned to its runtime release, so it is
	// upgraded only by changing its runtime, never on a passing test run.
	Pinned bool `json:"pinned,omitempty"`
	// LatestRun is its most recent test run; absent when it has none.
	LatestRun *runResponse `json:"latestRun,omitempty"`
	// LatestRunStale says the deployment was deployed or rolled out after that
	// run, so the run no longer speaks for what it runs.
	LatestRunStale bool `json:"latestRunStale,omitempty"`
}

// upgradesResponse is every deployment as an upgrade sees it.
type upgradesResponse struct {
	// RuntimeVersion is the runtime release this install deploys; empty when it
	// cannot say.
	RuntimeVersion string            `json:"runtimeVersion,omitempty"`
	Deployments    []upgradeResponse `json:"deployments"`
}

// upgrades godoc
//
//	@Summary		List deployments as an upgrade sees them
//	@Description	The runtime release this install deploys, and every deployment with the release it
//	@Description	runs, the newer one a rollout would move it onto, whether it has test suites to prove
//	@Description	that move, and its most recent test run.
//	@Tags			deployments
//	@Produce		json
//	@Success		200	{object}	upgradesResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/upgrades [get]
func (h *Handler) upgrades(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	current, items, err := h.svc.Upgrades(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	out := upgradesResponse{RuntimeVersion: current, Deployments: make([]upgradeResponse, 0, len(items))}
	for _, u := range items {
		resp := upgradeResponse{
			DeploymentID:   u.Deployment.ID,
			IntegrationID:  u.Deployment.IntegrationID,
			Name:           u.Name,
			Status:         u.Deployment.Status,
			Tag:            u.Tag,
			SnapshotID:     u.SnapshotID,
			RuntimeVersion: u.RuntimeVersion,
			UpgradeTo:      u.UpgradeTo,
			HasSuites:      u.HasSuites,
			LatestRunStale: u.LatestRunStale,
			Pinned:         u.Pinned,
		}
		if u.LatestRun != nil {
			run := toResponse(*u.LatestRun)
			resp.LatestRun = &run
		}
		out.Deployments = append(out.Deployments, resp)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}
