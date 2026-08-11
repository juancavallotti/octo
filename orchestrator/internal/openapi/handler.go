package openapi

import (
	"log/slog"
	"net/http"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// Handler serves the API description.
type Handler struct{}

// NewHandler returns a Handler.
func NewHandler() *Handler { return &Handler{} }

// Register attaches the routes to mux.
//
// These are registered outside the database gate: the description of an API is
// true whether or not that API's storage is wired, and an install still coming up
// is exactly when someone wants to read it.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", h.document)
	mux.HandleFunc("GET /openapi/operations", h.operations)
}

// document serves the description, optionally narrowed.
//
//	@Summary		The API description
//	@Description	This API as an OpenAPI 3.1 document. Narrow it with tag or path — the
//	@Description	full document is mostly schemas, and a filtered one carries only the
//	@Description	schemas its operations can reach. The two filters are alternatives, not
//	@Description	a combination.
//	@Tags			openapi
//	@Produce		json
//	@Param			tag		query	string	false	"Keep only operations carrying this tag"
//	@Param			path	query	string	false	"Keep only routes starting with this prefix"
//	@Success		200		"the description, filtered when asked"
//	@Failure		400		{object}	httpx.ErrorResponse	"tag and path were both supplied"
//	@Router			/openapi.json [get]
func (h *Handler) document(w http.ResponseWriter, r *http.Request) {
	tag := r.URL.Query().Get("tag")
	path := r.URL.Query().Get("path")
	if tag != "" && path != "" {
		httpx.WriteError(w, http.StatusBadRequest,
			"filter by tag or by path, not both")
		return
	}

	out, err := Filter(Spec(), tag, path)
	if err != nil {
		slog.Error("openapi handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if _, err := w.Write(out); err != nil {
		slog.Error("openapi: write document", "error", err)
	}
}

// operations serves the compact route index.
//
//	@Summary		The route index
//	@Description	Every route as one line: method, path, summary and tags. Read this
//	@Description	first to find the tag or path worth asking the description about.
//	@Tags			openapi
//	@Produce		json
//	@Success		200	{array}		Operation
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/openapi/operations [get]
func (h *Handler) operations(w http.ResponseWriter, _ *http.Request) {
	index, err := Index(Spec())
	if err != nil {
		slog.Error("openapi handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, index)
}
