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

// The two content types this module sends. Opaque payloads — a KV value, an
// agent's working memory — go as octet-stream because the platform stores bytes
// it has no reason to interpret; everything else is JSON.
const (
	contentTypeHeader = "Content-Type"
	contentTypeBytes  = "application/octet-stream"
	contentTypeJSON   = "application/json"
)

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
		version, versionErr := requireVersion(routeOp(routeKVGet), resp)
		if versionErr != nil {
			return core.Entry{}, false, versionErr
		}
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
		headerVersion:     strconv.FormatInt(expectedVersion, 10),
		contentTypeHeader: contentTypeBytes,
	}
	//nolint:bodyclose // drainClose (deferred below) closes the body
	resp, err := s.c.do(ctx, routeKVSet, s.entryURL(routeKVSet, namespace, key), value, headers, s.c.timeout)
	if err != nil {
		return 0, err
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		version, versionErr := requireVersion(routeOp(routeKVSet), resp)
		if versionErr != nil {
			return 0, versionErr
		}
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

// parseVersion reads a version header, treating a missing or malformed value as
// 0. Use it only where 0 is a legitimate answer — a request the runtime is
// sending, or a fake reading one back. On a response that succeeded, use
// requireVersion.
func parseVersion(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// requireVersion reads the version off a response that succeeded, where a usable
// one is not optional.
//
// It used to be parseVersion, on the theory that a server which sent no header
// still round-tripped a create. It does — once. The create returns 0, the caller
// stores 0, and every later update sends 0, which means CREATE, so the server
// answers 409 and the object can never be written again. The failure surfaces as
// a permanent version conflict on a key that plainly exists, which points at
// everything except the missing header that caused it.
//
// So a successful response carrying no version, a malformed one, or one that is
// not positive is a protocol violation, and it is reported as one at the moment
// it happens.
func requireVersion(op string, resp *http.Response) (int64, error) {
	raw := resp.Header.Get(headerVersion)
	if raw == "" {
		return 0, fmt.Errorf("api %s: the platform API answered %s with no %s header; "+
			"without it this object could be created but never updated", op, resp.Status, headerVersion)
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("api %s: the platform API answered %s with %s: %q, which is not a number",
			op, resp.Status, headerVersion, raw)
	}
	if v <= 0 {
		return 0, fmt.Errorf("api %s: the platform API answered %s with %s: %d. A stored object's "+
			"version must be positive — 0 is the value that means \"create\" on the way in",
			op, resp.Status, headerVersion, v)
	}
	return v, nil
}
