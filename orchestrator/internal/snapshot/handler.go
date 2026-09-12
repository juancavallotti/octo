package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/juancavallotti/octo/orchestrator/internal/authz"
	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// requestTimeout bounds the database work behind a single request.
const requestTimeout = 5 * time.Second

// Handler serves the snapshot REST endpoints.
type Handler struct {
	svc *Service
	// deployments answers which integration a deployment belongs to, for the
	// ownership check on the resource routes. Nil where deployments are not served
	// at all, which is every install with no cluster.
	deployments deploymentOwner
}

// deploymentOwner is the one question the resource routes ask about a
// deployment. Declared here and satisfied by *deployment.Service, so this
// package does not depend on that one.
type deploymentOwner interface {
	IntegrationOf(ctx context.Context, deploymentID string) (string, error)
}

// ErrForeignSnapshot is returned when a deployment asks for resources belonging
// to an integration that is not its own.
var ErrForeignSnapshot = errors.New("this deployment may not read that integration's resources")

// RestrictToOwnIntegration makes the resource routes refuse a pod reaching for
// another integration's frozen files.
//
// Wired separately from construction because deployments are only served where
// there is a cluster, and these routes are served everywhere — a snapshot can be
// cut on an install that cannot deploy anything.
func (h *Handler) RestrictToOwnIntegration(d deploymentOwner) {
	h.deployments = d
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the snapshot routes to mux. Create/list are nested under an
// integration; delete addresses a snapshot directly. The resource routes serve
// the resources frozen alongside the definition: a deployed runtime reads them
// from the content route (kind and name are query params, not path segments,
// because a name is path-like and may contain '/').
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /integrations/{id}/snapshots", h.create)
	mux.HandleFunc("GET /integrations/{id}/snapshots", h.listByIntegration)
	mux.HandleFunc("DELETE /snapshots/{id}", h.delete)
	mux.HandleFunc("GET /snapshots/{id}/resources", h.listResources)
	mux.HandleFunc("GET /snapshots/{id}/resources/content", h.resourceContent)
}

// createRequest is the create payload: the tag to freeze the current definition
// under.
type createRequest struct {
	Tag string `json:"tag"`
}

// snapshotResponse is the wire representation of a snapshot, including its frozen
// definition so clients can show version-scoped stats (e.g. the detail pane's
// Definition panel) without a second fetch. ListByIntegration already loads it.
type snapshotResponse struct {
	ID            string    `json:"id"`
	IntegrationID string    `json:"integrationId"`
	Tag           string    `json:"tag"`
	Definition    string    `json:"definition"`
	CreatedAt     time.Time `json:"createdAt"`
}

// toResponse converts rather than copies field by field: the two structs differ
// only in their json tags, which a conversion ignores. Adding a field to either
// one breaks this line, which is where the decision about whether it belongs on
// the wire should be taken anyway.
func toResponse(s Snapshot) snapshotResponse {
	return snapshotResponse(s)
}

// create godoc
//
//	@Summary		Tag a version of an integration
//	@Description	Freezes the integration's current definition and a copy of its resources under a
//	@Description	tag. Tags are immutable and unique per integration; a deploy ships a tag's frozen
//	@Description	definition rather than the live one, which is what makes a running deployment
//	@Description	independent of later edits.
//	@Tags			snapshots
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string			true	"Integration id"
//	@Param			body	body		createRequest	true	"The tag to create"
//	@Success		201		{object}	snapshotResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"an invalid tag"
//	@Failure		404		{object}	httpx.ErrorResponse
//	@Failure		409		{object}	httpx.ErrorResponse	"the tag already exists"
//	@Router			/integrations/{id}/snapshots [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	s, err := h.svc.Create(ctx, r.PathValue("id"), req.Tag)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(s))
}

// listByIntegration godoc
//
//	@Summary		List an integration's version tags
//	@Tags			snapshots
//	@Produce		json
//	@Param			id	path		string	true	"Integration id"
//	@Success		200	{array}		snapshotResponse
//	@Failure		404	{object}	httpx.ErrorResponse
//	@Router			/integrations/{id}/snapshots [get]
func (h *Handler) listByIntegration(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	items, err := h.svc.ListByIntegration(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]snapshotResponse, 0, len(items))
	for _, s := range items {
		out = append(out, toResponse(s))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// delete godoc
//
//	@Summary		Delete a version tag
//	@Description	Refused while a deployment still references it — deleting it would leave that
//	@Description	deployment unable to say what it is running.
//	@Tags			snapshots
//	@Produce		json
//	@Param			id	path	string	true	"Snapshot id"
//	@Success		204	"deleted"
//	@Failure		404	{object}	httpx.ErrorResponse
//	@Failure		409	{object}	httpx.ErrorResponse	"a deployment is using it"
//	@Router			/snapshots/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.svc.Delete(ctx, r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resourceResponse is the wire representation of a frozen resource. Content is
// omitted from the listing (it may be large); the content route serves the raw
// bytes.
type resourceResponse struct {
	Kind      string    `json:"kind"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

// listResources godoc
//
//	@Summary		List the resources frozen under a tag
//	@Description	Metadata only. This is what a running runtime reads to discover its resources.
//	@Tags			snapshots
//	@Produce		json
//	@Param			id	path		string	true	"Snapshot id"
//	@Success		200	{array}		resourceResponse
//	@Failure		404	{object}	httpx.ErrorResponse
//	@Router			/snapshots/{id}/resources [get]
func (h *Handler) listResources(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.mayRead(ctx, r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	items, err := h.svc.ListResources(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]resourceResponse, 0, len(items))
	for _, res := range items {
		out = append(out, resourceResponse{Kind: res.Kind, Name: res.Name, CreatedAt: res.CreatedAt})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// resourceContent serves one frozen resource's raw bytes. kind and name are
// required query params; a missing resource is a 404. This is the endpoint the
// runtime's k8s resource loader calls.
//
//	@Summary		Read one frozen resource
//	@Description	Returns the bytes verbatim. Kind and name are query parameters rather than path
//	@Description	segments because a resource name may itself contain slashes.
//	@Tags			snapshots
//	@Produce		octet-stream
//	@Param			id		path	string	true	"Snapshot id"
//	@Param			kind	query	string	true	"Resource kind (env or template)"
//	@Param			name	query	string	true	"Resource name"
//	@Success		200		{string}	string	"the resource bytes"
//	@Failure		400		"kind or name is missing"
//	@Failure		404		"no such snapshot or resource"
//	@Router			/snapshots/{id}/resources/content [get]
func (h *Handler) resourceContent(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	name := r.URL.Query().Get("name")
	if kind == "" || name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "kind and name query params are required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.mayRead(ctx, r.PathValue("id")); err != nil {
		h.writeError(w, err)
		return
	}
	content, found, err := h.svc.ResourceContent(ctx, r.PathValue("id"), kind, name)
	if err != nil {
		h.writeError(w, err)
		return
	}
	if !found {
		httpx.WriteError(w, http.StatusNotFound, "resource not found")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if _, err := w.Write(content); err != nil {
		slog.Error("snapshot handler: write resource content", "error", err)
	}
}

// writeError maps domain errors to HTTP status codes. Unexpected errors are
// logged and reported generically so internals do not leak to clients.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrTagExists):
		httpx.WriteError(w, http.StatusConflict, "a snapshot with this tag already exists")
	case errors.Is(err, ErrIntegrationNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	case errors.Is(err, ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "snapshot not found")
	case errors.Is(err, ErrForeignSnapshot):
		httpx.WriteError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrSnapshotInUse):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	default:
		slog.Error("snapshot handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}

// mayRead refuses a deployment reaching for another integration's frozen
// resources.
//
// Only a token that is nothing but a pod is constrained. platform:runtime opens
// these routes to every deployment without anybody granting it — it is what the
// runtime needs to load its own definition — so without this, one pod's mounted
// credential reads every integration's frozen files. A deployment that also
// holds a person's role got it from an access grant somebody ticked on purpose,
// and that grant is installation-wide by definition; narrowing it here would
// take back what was deliberately given.
//
// Scoped to the integration rather than to the exact snapshot, deliberately. A
// rollout moves the deployment to a new snapshot while the old pods are still
// serving, and those pods load resources lazily — an exact match would refuse a
// pod its own files for the length of every rollout. What matters is that the
// files belong to the integration this pod is running, and that is stable across
// version changes.
func (h *Handler) mayRead(ctx context.Context, snapshotID string) error {
	principal, ok := authz.FromContext(ctx)
	if !ok || principal.Deployment == "" {
		// A person, or an install with enforcement off. Governed by roles alone.
		return nil
	}
	if principal.Has(authz.RoleAdmin, authz.RoleOperator, authz.RoleDeveloper, authz.RoleMonitor) {
		return nil
	}
	if h.deployments == nil {
		// A machine token naming a deployment, on an install that serves no
		// deployments. Nothing should be able to produce that, so it is refused
		// rather than waved through.
		return ErrForeignSnapshot
	}

	mine, err := h.deployments.IntegrationOf(ctx, principal.Deployment)
	if err != nil {
		return fmt.Errorf("resolve the caller's integration: %w", err)
	}
	snap, err := h.svc.Get(ctx, snapshotID)
	if err != nil {
		return err
	}
	if snap.IntegrationID != mine {
		slog.Warn("a deployment reached for another integration's resources",
			"deployment", principal.Deployment, "snapshot", snapshotID)
		return ErrForeignSnapshot
	}
	return nil
}
