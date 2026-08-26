package bundle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
	"github.com/juancavallotti/octo/orchestrator/internal/integration"
	"github.com/juancavallotti/octo/orchestrator/internal/resource"
	"github.com/juancavallotti/octo/orchestrator/internal/snapshot"
)

const (
	// requestTimeout bounds the database work behind a single request. Longer than
	// the single-record routes': a bundle is one integration plus every resource it
	// owns, so it is several round trips rather than one.
	requestTimeout = 30 * time.Second
	// maxUploadBytes caps the compressed upload. What it expands to is capped
	// separately by the reader, which is the limit that actually bounds memory.
	maxUploadBytes = 8 << 20 // 8 MiB
	// archiveContentType is what a bundle is served as and uploaded as.
	archiveContentType = "application/zip"
	// nameQueryParam names an import whose archive carries no manifest; the BFF
	// passes the uploaded file's stem. actorQueryParam carries the acting user, the
	// way the JSON routes carry it in the body — an upload's body is the archive, so
	// there is nowhere else for it to go.
	nameQueryParam  = "name"
	actorQueryParam = "actorId"
)

// Handler serves the bundle import/export endpoints.
type Handler struct {
	svc *Service
}

// NewHandler returns a Handler backed by svc.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register attaches the bundle routes to mux.
//
// Export hangs off the thing being exported — an integration or one of its
// version tags. Import is a POST to the integrations collection, because it
// creates an integration; replace is a PUT on one, because it overwrites the one
// it addresses.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /integrations/{id}/bundle", h.export)
	mux.HandleFunc("GET /snapshots/{id}/bundle", h.exportSnapshot)
	mux.HandleFunc("POST /integrations/bundle", h.importNew)
	mux.HandleFunc("PUT /integrations/{id}/bundle", h.replace)
}

// integrationResponse is the wire representation of the integration an import or
// a replace produced. Deliberately narrower than the integration module's own
// response: a bundle upload answers with what changed, and the caller re-reads
// the integration through its own endpoint for the rest.
type integrationResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Definition  string    `json:"definition"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// export godoc
//
// The failure responses carry the usual JSON { error } envelope, but are declared
// without a schema: swag applies one produced content type to every response of an
// operation, so naming the envelope here would describe it as application/zip.
//
//	@Summary		Download an integration as a bundle
//	@Description	A zip holding the integration's definition and every resource it owns, laid out the
//	@Description	way the runtime reads them from disk: the definition at the root, each resource at its
//	@Description	own path-like name, plus an `octo-bundle.json` manifest carrying the display name and
//	@Description	each resource's kind.
//	@Tags			bundles
//	@Produce		application/zip
//	@Param			id	path	string	true	"Integration id"
//	@Success		200	{string}	string	"the bundle archive"
//	@Failure		404	"no such integration"
//	@Failure		500	"internal error"
//	@Router			/integrations/{id}/bundle [get]
func (h *Handler) export(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	b, err := h.svc.Export(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeArchive(w, b)
}

// exportSnapshot godoc
//
//	@Summary		Download a version tag as a bundle
//	@Description	The same archive as an integration's bundle, built from the tag's frozen definition and
//	@Description	frozen resources rather than the working copy.
//	@Tags			bundles
//	@Produce		application/zip
//	@Param			id	path	string	true	"Snapshot id"
//	@Success		200	{string}	string	"the bundle archive"
//	@Failure		404	"no such snapshot"
//	@Failure		500	"internal error"
//	@Router			/snapshots/{id}/bundle [get]
func (h *Handler) exportSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	b, err := h.svc.ExportSnapshot(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeArchive(w, b)
}

// importNew godoc
//
//	@Summary		Import a bundle as a new integration
//	@Description	The request body is the zip itself. A bundle written by this API names the integration
//	@Description	through its manifest; for one without a manifest, the `name` query parameter names the
//	@Description	import (the caller usually passes the uploaded filename's stem). A name already in use
//	@Description	is suffixed rather than rejected.
//	@Tags			bundles
//	@Accept			application/zip
//	@Produce		json
//	@Param			name	query		string	false	"Name for an archive that carries no manifest"
//	@Param			actorId	query		string	false	"Acting user id, forwarded by the BFF"
//	@Success		201		{object}	integrationResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"the archive is not a readable bundle"
//	@Failure		409		{object}	httpx.ErrorResponse	"no free name could be found for the import"
//	@Failure		413		{object}	httpx.ErrorResponse	"the upload is too large"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/integrations/bundle [post]
func (h *Handler) importNew(w http.ResponseWriter, r *http.Request) {
	data, err := readUpload(w, r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	it, err := h.svc.Import(ctx, data, strings.TrimSpace(r.URL.Query().Get(nameQueryParam)), r.URL.Query().Get(actorQueryParam))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, toResponse(it))
}

// replace godoc
//
//	@Summary		Replace an integration from a bundle
//	@Description	Overwrites the addressed integration's definition and resource set with the archive's,
//	@Description	keeping its id, name, folder, version tags and deployments. Resources are reconciled by
//	@Description	name: shared names are updated in place, new ones added, and any the bundle no longer
//	@Description	carries are deleted. The bundle's own name is ignored — a replace is not a rename.
//	@Tags			bundles
//	@Accept			application/zip
//	@Produce		json
//	@Param			id		path		string	true	"Integration id"
//	@Param			actorId	query		string	false	"Acting user id, forwarded by the BFF"
//	@Success		200		{object}	integrationResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"the archive is not a readable bundle"
//	@Failure		404		{object}	httpx.ErrorResponse	"no such integration"
//	@Failure		409		{object}	httpx.ErrorResponse	"a resource name in the bundle conflicts"
//	@Failure		413		{object}	httpx.ErrorResponse	"the upload is too large"
//	@Failure		500		{object}	httpx.ErrorResponse
//	@Router			/integrations/{id}/bundle [put]
func (h *Handler) replace(w http.ResponseWriter, r *http.Request) {
	data, err := readUpload(w, r)
	if err != nil {
		h.writeError(w, err)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	it, err := h.svc.Replace(ctx, r.PathValue("id"), data, r.URL.Query().Get(actorQueryParam))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, toResponse(it))
}

// toResponse maps the created or replaced integration to its wire form.
func toResponse(it integration.Integration) integrationResponse {
	return integrationResponse{
		ID:          it.ID,
		Name:        it.Name,
		Definition:  it.Definition,
		LastUpdated: it.LastUpdated,
	}
}

// readUpload reads the whole archive into memory, bounded. A bundle has to be
// read as a unit anyway — a zip's index lives at its end — so there is nothing to
// gain from streaming it.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxUploadBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, fmt.Errorf("%w: the upload exceeds %d bytes", ErrTooLarge, maxUploadBytes)
		}
		return nil, fmt.Errorf("%w: the upload could not be read", ErrInvalid)
	}
	return data, nil
}

// writeArchive renders the bundle and serves it as a download named after the
// integration (and the tag, when the bundle came from one).
func (h *Handler) writeArchive(w http.ResponseWriter, b Bundle) {
	data, err := Write(b)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", archiveContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName(b)))
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	if _, err := w.Write(data); err != nil {
		// The status and headers are already out, so this can only be logged.
		slog.Error("bundle handler: write archive", "error", err)
	}
}

// downloadName is the filename a bundle is offered under: the integration's slug,
// suffixed with the tag when the export came from a version.
func downloadName(b Bundle) string {
	if b.Tag == "" {
		return Slug(b.Name) + ".zip"
	}
	return Slug(b.Name) + "-" + Slug(b.Tag) + ".zip"
}

// writeError maps domain errors to HTTP status codes. The errors come from three
// modules — a bundle spans all of them — so each module's "not found" is mapped
// here rather than assumed to be one shared error.
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTooLarge):
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, ErrInvalid), errors.Is(err, resource.ErrInvalid), errors.Is(err, integration.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, integration.ErrNameTaken), errors.Is(err, resource.ErrNameExists):
		httpx.WriteError(w, http.StatusConflict, err.Error())
	case errors.Is(err, integration.ErrNotFound), errors.Is(err, resource.ErrIntegrationNotFound):
		httpx.WriteError(w, http.StatusNotFound, "integration not found")
	case errors.Is(err, snapshot.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "snapshot not found")
	default:
		slog.Error("bundle handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
