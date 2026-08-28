package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/juancavallotti/octo/runtime/core"
)

// headerVersion carries the object version both ways: the server returns the
// current version on reads and the new version on writes, and the client sends
// the expected version on writes for the optimistic-concurrency check. It is the
// same header and the same meaning the k8s module uses, so a platform that has
// implemented one has implemented the other.
const headerVersion = "X-Object-Version"

// kvStore is the key-value store delegated to the platform API.
//
// The namespace is a path segment and carries its full suffixed name —
// user_secrets, system_volatile — because a backend routes on the name it was
// given, and nothing else about the contract has to change for a platform to gain
// a tier. The key is a query parameter rather than a segment: keys are path-like,
// and %2F inside a segment is silently normalized by nginx, by Cloud Run's front
// end and by several frameworks, which would merge two distinct keys into one.
type kvStore struct {
	c     *client
	latch *latch
	// maxValueBytes is the server's declared limit, checked here so an oversized
	// write fails with a message naming the limit rather than as an opaque 413.
	maxValueBytes int64
}

func newKVStore(c *client, f kvFeature) *kvStore {
	return &kvStore{c: c, latch: &latch{feature: FeatureKV}, maxValueBytes: f.MaxValueBytes}
}

// entryURL addresses one key in one namespace.
func (s *kvStore) entryURL(r route, namespace, key string) string {
	return query(s.c.url(r, namespace), "key", key)
}

func (s *kvStore) Get(ctx context.Context, namespace, key string) (core.Entry, bool, error) {
	if !s.latch.live() {
		return core.Entry{}, false, nil
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := s.c.do(ctx, routeKVGet, s.entryURL(routeKVGet, namespace, key), nil, nil, s.c.timeout)
	if err != nil {
		return core.Entry{}, false, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		value, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return core.Entry{}, false, fmt.Errorf("api kv get: read body: %w", readErr)
		}
		version := parseVersion(resp.Header.Get(headerVersion))
		slog.Debug("api kv get hit", "namespace", namespace, "key", key, "version", version)
		return core.Entry{Value: value, Version: version}, true, nil
	case http.StatusNotFound:
		// A miss, not a failure: 404 in this contract means the addressed thing is
		// absent, never that the route is unknown.
		slog.Debug("api kv get miss", "namespace", namespace, "key", key)
		return core.Entry{}, false, nil
	case http.StatusNotImplemented:
		s.latch.mark()
		return core.Entry{}, false, nil
	default:
		return core.Entry{}, false, statusError(routeOp(routeKVGet), resp)
	}
}

func (s *kvStore) Set(
	ctx context.Context, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	if !s.latch.live() {
		return 0, core.ErrNoKV
	}
	if s.maxValueBytes > 0 && int64(len(value)) > s.maxValueBytes {
		return 0, fmt.Errorf("api kv set: value is %d bytes and the platform API declared a "+
			"limit of %d", len(value), s.maxValueBytes)
	}
	headers := map[string]string{
		headerVersion:  strconv.FormatInt(expectedVersion, 10),
		"Content-Type": "application/octet-stream",
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := s.c.do(ctx, routeKVSet, s.entryURL(routeKVSet, namespace, key), value, headers, s.c.timeout)
	if err != nil {
		return 0, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		version := parseVersion(resp.Header.Get(headerVersion))
		slog.Debug("api kv set ok", "namespace", namespace, "key", key, "version", version)
		return version, nil
	case http.StatusConflict:
		return 0, core.ErrVersionConflict
	case http.StatusNotImplemented:
		s.latch.mark()
		return 0, core.ErrNoKV
	default:
		return 0, statusError(routeOp(routeKVSet), resp)
	}
}

func (s *kvStore) Delete(ctx context.Context, namespace, key string, expectedVersion int64) error {
	if !s.latch.live() {
		return core.ErrNoKV
	}
	headers := map[string]string{headerVersion: strconv.FormatInt(expectedVersion, 10)}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := s.c.do(ctx, routeKVDelete, s.entryURL(routeKVDelete, namespace, key), nil, headers, s.c.timeout)
	if err != nil {
		return err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	// A delete of something that is not there has achieved what the caller asked.
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	case http.StatusConflict:
		return core.ErrVersionConflict
	case http.StatusNotImplemented:
		s.latch.mark()
		return core.ErrNoKV
	default:
		return statusError(routeOp(routeKVDelete), resp)
	}
}

// parseVersion reads a version header, treating a missing or malformed value as 0
// — which is also the value that means "create" on a write, so a server that does
// not send the header still round-trips a create.
func parseVersion(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}
