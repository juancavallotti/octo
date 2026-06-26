package k8s

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/juancavallotti/octo/core"
)

// headerVersion carries the object version both ways: the server returns the
// current version on reads and the new version on writes; the client sends the
// expected version on writes for the optimistic-concurrency check.
const headerVersion = "X-Object-Version"

// kvClient talks to the orchestrator KV API, which stores values scoped to this
// deployment and encrypts the ones written as secrets. Keys are namespaced in the
// request path.
type kvClient struct {
	baseURL      string
	deploymentID string
	token        string
	http         *http.Client
}

func newKVClient(baseURL, deploymentID, token string) *kvClient {
	return &kvClient{
		baseURL:      strings.TrimRight(baseURL, "/"),
		deploymentID: deploymentID,
		token:        token,
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *kvClient) close() { c.http.CloseIdleConnections() }

// endpoint builds the URL for a namespaced key under this deployment.
func (c *kvClient) endpoint(namespace, key string) string {
	return fmt.Sprintf("%s/api/deployments/%s/kv/%s/%s",
		c.baseURL,
		url.PathEscape(c.deploymentID),
		url.PathEscape(namespace),
		url.PathEscape(key))
}

func (c *kvClient) Get(ctx context.Context, namespace, key string) (core.Entry, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(namespace, key), nil)
	if err != nil {
		return core.Entry{}, false, err
	}
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return core.Entry{}, false, fmt.Errorf("kv get: %w", err)
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		value, err := io.ReadAll(resp.Body)
		if err != nil {
			return core.Entry{}, false, fmt.Errorf("kv get: read body: %w", err)
		}
		version := parseVersion(resp.Header.Get(headerVersion))
		slog.Debug("kv get hit", "namespace", namespace, "key", key, "version", version, "bytes", len(value))
		return core.Entry{Value: value, Version: version}, true, nil
	case http.StatusNotFound:
		slog.Debug("kv get miss", "namespace", namespace, "key", key)
		return core.Entry{}, false, nil
	default:
		return core.Entry{}, false, statusError("kv get", resp)
	}
}

func (c *kvClient) Set(ctx context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	return c.put(ctx, namespace, key, value, expectedVersion, false)
}

func (c *kvClient) SetSecret(ctx context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	return c.put(ctx, namespace, key, value, expectedVersion, true)
}

// put writes value under namespace/key. secret=true asks the orchestrator to
// encrypt it at rest. A 409 maps to core.ErrVersionConflict so callers refresh and
// retry.
func (c *kvClient) put(
	ctx context.Context, namespace, key string, value []byte, expectedVersion int64, secret bool,
) (int64, error) {
	endpoint := c.endpoint(namespace, key)
	if secret {
		endpoint += "?secret=true"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(value))
	if err != nil {
		return 0, err
	}
	req.Header.Set(headerVersion, strconv.FormatInt(expectedVersion, 10))
	req.Header.Set("Content-Type", "application/octet-stream")
	c.authorize(req)

	slog.Debug("kv set", "namespace", namespace, "key", key,
		"expectedVersion", expectedVersion, "secret", secret, "bytes", len(value))

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("kv set: %w", err)
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		version := parseVersion(resp.Header.Get(headerVersion))
		slog.Debug("kv set ok", "namespace", namespace, "key", key, "version", version)
		return version, nil
	case http.StatusConflict:
		slog.Debug("kv set conflict", "namespace", namespace, "key", key, "expectedVersion", expectedVersion)
		return 0, core.ErrVersionConflict
	default:
		return 0, statusError("kv set", resp)
	}
}

func (c *kvClient) Delete(ctx context.Context, namespace, key string, expectedVersion int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint(namespace, key), nil)
	if err != nil {
		return err
	}
	req.Header.Set(headerVersion, strconv.FormatInt(expectedVersion, 10))
	c.authorize(req)

	slog.Debug("kv delete", "namespace", namespace, "key", key, "expectedVersion", expectedVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kv delete: %w", err)
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	case http.StatusConflict:
		return core.ErrVersionConflict
	default:
		return statusError("kv delete", resp)
	}
}

// authorize attaches the bearer token when one is configured.
func (c *kvClient) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// parseVersion reads a version header, treating a missing or malformed value as 0.
func parseVersion(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// statusError builds an error from an unexpected response, including a short snippet
// of the body for context.
func statusError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("%s: unexpected status %s", op, resp.Status)
	}
	return fmt.Errorf("%s: unexpected status %s: %s", op, resp.Status, msg)
}

// drainClose drains and closes a response body so the connection can be reused.
func drainClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
