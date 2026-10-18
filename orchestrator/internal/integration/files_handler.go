package integration

import (
	"context"
	"net/http"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// pathParam names the query parameter that addresses one config file. A path is
// path-like, so it never goes in a URL path segment.
const pathParam = "path"

// actorParam names the optional query parameter that attributes a delete, which
// carries no body to put it in.
const actorParam = "actorId"

// fileSummaryResponse is one entry of the config file listing. Content is left
// out; the content route serves it.
type fileSummaryResponse struct {
	// Path is the file's name at the project root, such as "orders.yaml".
	Path        string    `json:"path"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// fileResponse is one config file with its content.
type fileResponse struct {
	// Path is the file's name at the project root, such as "orders.yaml".
	Path        string    `json:"path"`
	Content     string    `json:"content"`
	LastUpdated time.Time `json:"lastUpdated"`
}

// fileRequest is the payload that writes a config file. ActorID is the acting
// user's id, empty when unknown: attribution carried in the body, not a verified
// credential.
type fileRequest struct {
	Content string `json:"content"`
	ActorID string `json:"actorId"`
}

// listFiles godoc
//
//	@Summary		List an integration's config files
//	@Description	The root-level YAML files the runtime merges into one config, ordered by
//	@Description	path. Metadata only; the content route serves each file.
//	@Tags			integrations
//	@Produce		json
//	@Param			id	path		string	true	"Integration id"
//	@Success		200	{array}		fileSummaryResponse
//	@Failure		404	{object}	httpx.ErrorResponse	"no such integration"
//	@Router			/integrations/{id}/files [get]
func (h *Handler) listFiles(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	files, err := h.svc.ListFiles(ctx, r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]fileSummaryResponse, 0, len(files))
	for _, f := range files {
		out = append(out, fileSummaryResponse{Path: f.Path, LastUpdated: f.LastUpdated})
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// getFile godoc
//
//	@Summary	Get one config file
//	@Tags		integrations
//	@Produce	json
//	@Param		id		path		string	true	"Integration id"
//	@Param		path	query		string	true	"The file's path, such as orders.yaml"
//	@Success	200		{object}	fileResponse
//	@Failure	400		{object}	httpx.ErrorResponse	"the path is not a config file"
//	@Failure	404		{object}	httpx.ErrorResponse	"no such integration or file"
//	@Router		/integrations/{id}/files/content [get]
func (h *Handler) getFile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	f, err := h.svc.GetFile(ctx, r.PathValue("id"), r.URL.Query().Get(pathParam))
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, fileResponse(f))
}

// putFile godoc
//
//	@Summary		Write one config file
//	@Description	Creates the file or replaces its content. The path must be a .yaml or .yml
//	@Description	file at the project root that does not end in _test. A write that would
//	@Description	leave the files unable to merge into one config — a connector, processor
//	@Description	or flow name used in two files, or a second service — is refused.
//	@Tags			integrations
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string		true	"Integration id"
//	@Param			path	query		string		true	"The file's path, such as orders.yaml"
//	@Param			body	body		fileRequest	true	"Content and acting user"
//	@Success		200		{object}	fileResponse
//	@Failure		400		{object}	httpx.ErrorResponse	"the path is not a config file"
//	@Failure		404		{object}	httpx.ErrorResponse	"no such integration"
//	@Failure		409		{object}	httpx.ErrorResponse	"the files would not merge"
//	@Router			/integrations/{id}/files/content [put]
func (h *Handler) putFile(w http.ResponseWriter, r *http.Request) {
	var req fileRequest
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	f, err := h.svc.PutFile(ctx, r.PathValue("id"), r.URL.Query().Get(pathParam), req.Content, req.ActorID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, fileResponse(f))
}

// deleteFile godoc
//
//	@Summary	Delete one config file
//	@Tags		integrations
//	@Produce	json
//	@Param		id		path	string	true	"Integration id"
//	@Param		path	query	string	true	"The file's path, such as orders.yaml"
//	@Param		actorId	query	string	false	"The acting user's id"
//	@Success	204		"deleted"
//	@Failure	400		{object}	httpx.ErrorResponse	"the path is not a config file"
//	@Failure	404		{object}	httpx.ErrorResponse	"no such integration or file"
//	@Router		/integrations/{id}/files/content [delete]
func (h *Handler) deleteFile(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	query := r.URL.Query()
	if err := h.svc.DeleteFile(ctx, r.PathValue("id"), query.Get(pathParam), query.Get(actorParam)); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
