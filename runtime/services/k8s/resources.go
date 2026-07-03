package k8s

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/juancavallotti/octo/core"
)

// resourceHTTPTimeout bounds each resource fetch from the orchestrator. Resources
// are read once per generation (a frozen snapshot never changes), so a generous
// timeout is fine.
const resourceHTTPTimeout = 10 * time.Second

// httpResourceLoader reads a deployment's resources from the orchestrator by the
// snapshot they were frozen under. It is the k8s counterpart to the standalone
// filesystem loader: the runtime asks for a resource by kind and id, and this
// fetches the frozen bytes over HTTP. It deliberately does not implement
// core.ResourceWatcher — a frozen snapshot is immutable, so there is nothing to
// watch.
type httpResourceLoader struct {
	baseURL    string
	snapshotID string
	token      string
	http       *http.Client
}

// newHTTPResourceLoader builds a loader for one deployment's snapshot.
func newHTTPResourceLoader(baseURL, snapshotID, token string) *httpResourceLoader {
	return &httpResourceLoader{
		baseURL:    strings.TrimRight(baseURL, "/"),
		snapshotID: snapshotID,
		token:      token,
		http:       &http.Client{Timeout: resourceHTTPTimeout},
	}
}

func (l *httpResourceLoader) close() { l.http.CloseIdleConnections() }

// endpoint builds the frozen-resource content URL for a kind/id, matching the
// orchestrator's snapshot routes. kind and id ride as query params (not path
// segments) because an id is path-like and may contain '/'.
func (l *httpResourceLoader) endpoint(kind core.ResourceKind, id string) string {
	q := url.Values{"kind": {string(kind)}, "name": {id}}
	return fmt.Sprintf("%s/snapshots/%s/resources/content?%s",
		l.baseURL, url.PathEscape(l.snapshotID), q.Encode())
}

// Load fetches the bytes for the resource id of the given kind from the
// orchestrator, or core.ErrResourceNotFound when the snapshot has no such
// resource.
func (l *httpResourceLoader) Load(ctx context.Context, kind core.ResourceKind, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.endpoint(kind, id), nil)
	if err != nil {
		return nil, fmt.Errorf("resource load: new request: %w", err)
	}
	if l.token != "" {
		req.Header.Set("Authorization", "Bearer "+l.token)
	}

	resp, err := l.http.Do(req) //nolint:bodyclose // drainClose (deferred below) closes the body
	if err != nil {
		return nil, fmt.Errorf("resource load: %w", err)
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("resource load: read body: %w", err)
		}
		slog.Debug("resource load hit", "kind", kind, "id", id, "bytes", len(body))
		return body, nil
	case http.StatusNotFound:
		slog.Debug("resource load miss", "kind", kind, "id", id)
		return nil, core.ErrResourceNotFound
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, errorBodySnippet))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			return nil, fmt.Errorf("resource load: unexpected status %s", resp.Status)
		}
		return nil, fmt.Errorf("resource load: unexpected status %s: %s", resp.Status, msg)
	}
}
