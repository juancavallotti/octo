package kv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	httpx "github.com/juancavallotti/octo/orchestrator/internal/http"
)

const (
	// requestTimeout bounds the database work behind a single request.
	requestTimeout = 5 * time.Second
	// maxValueBytes caps a stored value so a client cannot force unbounded reads.
	maxValueBytes = 1 << 20 // 1 MiB
	// headerVersion carries the object version in both directions.
	headerVersion = "X-Object-Version"
)

// Store is the operation surface a Handler serves; *Service satisfies it.
type Store interface {
	Get(ctx context.Context, deploymentID, namespace, key string) ([]byte, int64, bool, error)
	Set(ctx context.Context, deploymentID, namespace, key string, value []byte, expectedVersion int64) (int64, error)
	Delete(ctx context.Context, deploymentID, namespace, key string, expectedVersion int64) error
}

// Handler serves the deployment-scoped KV routes. The secret store is not a separate
// endpoint: secrets are keys in secret namespaces, encrypted by the service.
type Handler struct {
	store Store
}

// NewHandler returns a Handler serving store.
func NewHandler(store Store) *Handler {
	return &Handler{store: store}
}

// Register attaches the KV routes to mux. The key segment is a trailing wildcard so
// keys may contain slashes.
func (h *Handler) Register(mux *http.ServeMux) {
	const base = "/deployments/{id}/kv/{namespace}/{key...}"
	mux.HandleFunc("GET "+base, h.get)
	mux.HandleFunc("PUT "+base, h.put)
	mux.HandleFunc("DELETE "+base, h.delete)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	value, version, ok, err := h.store.Get(ctx, r.PathValue("id"), r.PathValue("namespace"), r.PathValue("key"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set(headerVersion, strconv.FormatInt(version, 10))
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(value)
}

func (h *Handler) put(w http.ResponseWriter, r *http.Request) {
	expected, err := versionHeader(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid "+headerVersion+" header")
		return
	}
	value, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxValueBytes))
	if err != nil {
		httpx.WriteError(w, http.StatusRequestEntityTooLarge, "value too large")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	version, err := h.store.Set(ctx, r.PathValue("id"), r.PathValue("namespace"), r.PathValue("key"), value, expected)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set(headerVersion, strconv.FormatInt(version, 10))
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	expected, err := versionHeader(r)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "invalid "+headerVersion+" header")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	if err := h.store.Delete(ctx, r.PathValue("id"), r.PathValue("namespace"), r.PathValue("key"), expected); err != nil {
		h.writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// versionHeader reads the X-Object-Version header, defaulting an absent header to 0
// (create / unconditional).
func versionHeader(r *http.Request) (int64, error) {
	raw := r.Header.Get(headerVersion)
	if raw == "" {
		return 0, nil
	}
	return strconv.ParseInt(raw, 10, 64)
}

// writeError maps domain errors to HTTP status codes. A version conflict is a 409
// so the runtime client maps it back to its conflict error; a secret write without
// a configured key is a 503 (the feature is unavailable, not the caller's fault).
func (h *Handler) writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrVersionConflict):
		httpx.WriteError(w, http.StatusConflict, "version conflict")
	case errors.Is(err, ErrEncryptionDisabled):
		httpx.WriteError(w, http.StatusServiceUnavailable, "secret storage is not configured")
	default:
		slog.Error("kv handler", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal error")
	}
}
