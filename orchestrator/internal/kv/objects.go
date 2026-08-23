package kv

import (
	"context"
	"encoding/base64"
	"net/http"
	"strconv"
	"unicode/utf8"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

// userNamespace is the default namespace the object browser serves when a request
// names none, and userVolatileNamespace is its volatile counterpart. Both are
// advertised by the namespaces route whether or not they hold anything yet, so the
// browser's picker offers the two tiers a flow can write to rather than only the
// ones something happens to have written already. The handler serves any namespace named via ?namespace=, including the
// encrypted secret ones — but only their key metadata (list) and deletion (cleanup):
// it never reads or writes a secret value, so plaintext/ciphertext never reaches the
// platform's plain object API.
const (
	userNamespace         = "user"
	userVolatileNamespace = userNamespace + volatileNamespaceSuffix
)

// ObjectHandler serves the object browser the platform UI calls: a JSON facade over
// the KV store for inspecting and troubleshooting deployment state. It adds the
// namespace + key listing the raw KV routes lack; reads/writes/deletes otherwise
// mirror the raw handler's optimistic-concurrency semantics, with the version carried
// in the JSON body / query string rather than a header. Secret namespaces are listed
// and deletable (so stale credentials can be cleaned up) but their values are never
// served or edited here — that stays with the dedicated secrets API.
type ObjectHandler struct {
	store Store
}

// NewObjectHandler returns an ObjectHandler serving store.
func NewObjectHandler(store Store) *ObjectHandler {
	return &ObjectHandler{store: store}
}

// Register attaches the object routes to mux. The key segment is a trailing wildcard
// so keys may contain slashes.
func (h *ObjectHandler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /deployments/{id}/namespaces", h.namespaces)
	mux.HandleFunc("GET /deployments/{id}/objects", h.list)
	mux.HandleFunc("GET /deployments/{id}/objects/{key...}", h.get)
	mux.HandleFunc("PUT /deployments/{id}/objects/{key...}", h.put)
	mux.HandleFunc("DELETE /deployments/{id}/objects/{key...}", h.delete)
}

// namespaces lists the namespaces a deployment holds data in, so the browser can
// offer a picker. Secret namespaces are included — the browser can list and clean up
// their keys (just not view/edit values), which is the point of the troubleshooting
// view.
//
//	@Summary		List a deployment's namespaces
//	@Description	Secret namespaces are included: their keys can be listed and deleted, just not read.
//	@Tags			objects
//	@Produce		json
//	@Param			id	path	string	true	"Deployment id"
//	@Success		200	{object}	namespacesResponse
//	@Failure		500	{object}	httpx.ErrorResponse
//	@Router			/deployments/{id}/namespaces [get]
func (h *ObjectHandler) namespaces(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	all, err := h.store.ListNamespaces(ctx, r.PathValue("id"))
	if err != nil {
		writeStoreError(w, err)
		return
	}
	// Always advertise the user namespaces, even before anything is written to them.
	out := []string{userNamespace, userVolatileNamespace}
	for _, ns := range all {
		if ns != userNamespace && ns != userVolatileNamespace {
			out = append(out, ns)
		}
	}
	httpx.WriteJSON(w, http.StatusOK, namespacesResponse{Items: out})
}

// namespacesResponse and objectListResponse are the list envelopes. Named types
// rather than inline maps so the API description has a shape to point at: a caller
// reading the description otherwise learns only that something comes back.
type namespacesResponse struct {
	Items []string `json:"items"`
}

type objectListResponse struct {
	Items []Entry `json:"items"`
}

// objectValue is the JSON shape a single object is returned/written as. Encoding is
// "utf8" for human-readable values or "base64" for binary ones, so a value round-
// trips losslessly through JSON either way.
type objectValue struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Encoding string `json:"encoding"`
	Version  int64  `json:"version"`
}

// list godoc
//
//	@Summary		List the objects in a namespace
//	@Description	Key metadata only, no values, which is why secret namespaces are allowed here.
//	@Tags			objects
//	@Produce		json
//	@Param			id			path	string	true	"Deployment id"
//	@Param			namespace	query	string	false	"Namespace; defaults to the user namespace"
//	@Success		200			{object}	objectListResponse
//	@Failure		500			{object}	httpx.ErrorResponse
//	@Router			/deployments/{id}/objects [get]
func (h *ObjectHandler) list(w http.ResponseWriter, r *http.Request) {
	// Listing exposes only key metadata (no values), so secret namespaces are fine.
	namespace, ok := objectNamespace(w, r, true)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	entries, err := h.store.List(ctx, r.PathValue("id"), namespace)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if entries == nil {
		entries = []Entry{}
	}
	httpx.WriteJSON(w, http.StatusOK, objectListResponse{Items: entries})
}

// get godoc
//
//	@Summary		Read one object
//	@Description	Encoding is utf8 for readable values and base64 for binary ones, so a value round
//	@Description	trips losslessly either way. Secret namespaces are refused: this returns values.
//	@Tags			objects
//	@Produce		json
//	@Param			id			path		string	true	"Deployment id"
//	@Param			namespace	query		string	false	"Namespace; defaults to the user namespace"
//	@Param			key			path		string	true	"Key (may contain slashes)"
//	@Success		200			{object}	objectValue
//	@Failure		400			{object}	httpx.ErrorResponse	"a secret namespace"
//	@Failure		404			{object}	httpx.ErrorResponse
//	@Router			/deployments/{id}/objects/{key} [get]
func (h *ObjectHandler) get(w http.ResponseWriter, r *http.Request) {
	// Reading a value: secret namespaces are refused so plaintext never leaks.
	namespace, ok := objectNamespace(w, r, false)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	key := r.PathValue("key")
	value, version, ok, err := h.store.Get(ctx, r.PathValue("id"), namespace, key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	out := objectValue{Key: key, Version: version}
	// Return readable values verbatim; fall back to base64 for binary so JSON stays
	// valid and the value survives the round trip.
	if utf8.Valid(value) {
		out.Encoding = "utf8"
		out.Value = string(value)
	} else {
		out.Encoding = "base64"
		out.Value = base64.StdEncoding.EncodeToString(value)
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// put godoc
//
//	@Summary	Write one object
//	@Tags		objects
//	@Accept		json
//	@Produce	json
//	@Param		id			path		string		true	"Deployment id"
//	@Param		namespace	query		string		false	"Namespace; defaults to the user namespace"
//	@Param		key			path		string		true	"Key (may contain slashes)"
//	@Param		body		body		objectValue	true	"Value, encoding and expected version"
//	@Success	200			{object}	objectValue
//	@Failure	400			{object}	httpx.ErrorResponse
//	@Failure	409			{object}	httpx.ErrorResponse	"the version did not match"
//	@Router		/deployments/{id}/objects/{key} [put]
func (h *ObjectHandler) put(w http.ResponseWriter, r *http.Request) {
	// Writing a value: secret namespaces are refused (edit them via the secrets API).
	namespace, ok := objectNamespace(w, r, false)
	if !ok {
		return
	}

	var req objectValue
	if err := httpx.DecodeJSON(w, r, &req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	value, err := decodeValue(req.Value, req.Encoding)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid value encoding")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	version, err := h.store.Set(ctx, r.PathValue("id"), namespace, r.PathValue("key"), value, req.Version)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]int64{"version": version})
}

// delete godoc
//
//	@Summary	Delete one object
//	@Tags		objects
//	@Produce	json
//	@Param		id			path	string	true	"Deployment id"
//	@Param		namespace	query	string	false	"Namespace; defaults to the user namespace"
//	@Param		key			path	string	true	"Key (may contain slashes)"
//	@Success	204			"deleted"
//	@Failure	409			{object}	httpx.ErrorResponse	"the version did not match"
//	@Router		/deployments/{id}/objects/{key} [delete]
func (h *ObjectHandler) delete(w http.ResponseWriter, r *http.Request) {
	// Deleting removes a key (cleanup) without exposing its value, so secret
	// namespaces are allowed — e.g. to clear stale OAuth credentials.
	namespace, ok := objectNamespace(w, r, true)
	if !ok {
		return
	}

	expected, err := versionQuery(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid version")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.store.Delete(ctx, r.PathValue("id"), namespace, r.PathValue("key"), expected); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// objectNamespace resolves the namespace a request targets from ?namespace=,
// defaulting to the user namespace. When allowSecret is false it refuses the
// encrypted secret namespaces (writing a 400, returning ok=false so the caller
// stops) so their values are never read or written here; list and delete pass
// allowSecret=true since they only expose key metadata or remove a key.
func objectNamespace(w http.ResponseWriter, r *http.Request, allowSecret bool) (string, bool) {
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		return userNamespace, true
	}
	if !allowSecret && isSecret(namespace) {
		httpx.WriteError(w, http.StatusBadRequest, "secret values are not accessible here")
		return "", false
	}
	return namespace, true
}

// decodeValue turns the JSON value into bytes per its encoding ("base64" for binary,
// anything else treated as a literal UTF-8 string).
func decodeValue(value, encoding string) ([]byte, error) {
	if encoding == "base64" {
		return base64.StdEncoding.DecodeString(value)
	}
	return []byte(value), nil
}

// versionQuery reads the ?version= query param, defaulting an absent value to 0
// (unconditional).
func versionQuery(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("version")
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}
