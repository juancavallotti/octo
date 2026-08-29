package api

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/juancavallotti/octo/runtime/core"
)

// resourceLoader reads a runtime's resources — .env files and templates — from
// the platform API.
//
// Like the k8s loader it deliberately does not implement core.ResourceWatcher.
// Watching means polling somebody else's API forever for a change that, in this
// deployment shape, arrives as a redeploy anyway; not implementing the interface
// is how a loader opts out.
//
// kind and name ride as query parameters, not path segments, for the reason the
// k8s loader gives: a resource name is path-like and may contain a slash.
type resourceLoader struct {
	c     *client
	latch *latch
}

func newResourceLoader(c *client) *resourceLoader {
	return &resourceLoader{c: c, latch: &latch{feature: FeatureResources}}
}

// Load fetches the bytes for one resource, or core.ErrResourceNotFound.
func (l *resourceLoader) Load(ctx context.Context, kind core.ResourceKind, id string) ([]byte, error) {
	if !l.latch.live() {
		return nil, core.ErrResourceNotFound
	}
	endpoint := query(l.c.url(routeResourceContent), "kind", string(kind), "name", id)
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := l.c.do(ctx, routeResourceContent, endpoint, nil, nil, l.c.timeout)
	if err != nil {
		return nil, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, fmt.Errorf("api resources load: read body: %w", readErr)
		}
		return body, nil
	case http.StatusNotFound:
		return nil, core.ErrResourceNotFound
	case http.StatusNotImplemented:
		l.latch.mark()
		return nil, core.ErrResourceNotFound
	default:
		return nil, statusError(routeOp(routeResourceContent), resp)
	}
}
