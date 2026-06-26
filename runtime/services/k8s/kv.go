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

// httpStore is the deployment-scoped KV store backed by the orchestrator endpoint.
// The secret store rides on top of it (core.NewSecretStore) by writing to encrypted
// namespaces, so this one client serves both. Values are scoped to this deployment
// and namespaced in the path.
type httpStore struct {
	baseURL      string
	deploymentID string
	token        string
	http         *http.Client
}

func newHTTPStore(baseURL, deploymentID, token string) *httpStore {
	return &httpStore{
		baseURL:      strings.TrimRight(baseURL, "/"),
		deploymentID: deploymentID,
		token:        token,
		http:         &http.Client{Timeout: 10 * time.Second},
	}
}

func (c *httpStore) close() { c.http.CloseIdleConnections() }

// endpoint builds the URL for a namespaced key under this deployment, matching the
// orchestrator's KV routes.
func (c *httpStore) endpoint(namespace, key string) string {
	return fmt.Sprintf("%s/deployments/%s/kv/%s/%s",
		c.baseURL,
		url.PathEscape(c.deploymentID),
		url.PathEscape(namespace),
		url.PathEscape(key))
}

func (c *httpStore) Get(ctx context.Context, namespace, key string) (core.Entry, bool, error) {
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
		slog.Debug("store get hit", "namespace", namespace, "key", key, "version", version)
		return core.Entry{Value: value, Version: version}, true, nil
	case http.StatusNotFound:
		slog.Debug("store get miss", "namespace", namespace, "key", key)
		return core.Entry{}, false, nil
	default:
		return core.Entry{}, false, c.statusError("get", resp)
	}
}

func (c *httpStore) Set(ctx context.Context, namespace, key string, value []byte, expectedVersion int64) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint(namespace, key), bytes.NewReader(value))
	if err != nil {
		return 0, err
	}
	req.Header.Set(headerVersion, strconv.FormatInt(expectedVersion, 10))
	req.Header.Set("Content-Type", "application/octet-stream")
	c.authorize(req)

	slog.Debug("store set", "namespace", namespace, "key", key,
		"expectedVersion", expectedVersion, "bytes", len(value))

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("kv set: %w", err)
	}
	defer drainClose(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		version := parseVersion(resp.Header.Get(headerVersion))
		slog.Debug("store set ok", "namespace", namespace, "key", key, "version", version)
		return version, nil
	case http.StatusConflict:
		slog.Debug("store set conflict", "namespace", namespace, "key", key)
		return 0, core.ErrVersionConflict
	default:
		return 0, c.statusError("set", resp)
	}
}

func (c *httpStore) Delete(ctx context.Context, namespace, key string, expectedVersion int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint(namespace, key), nil)
	if err != nil {
		return err
	}
	req.Header.Set(headerVersion, strconv.FormatInt(expectedVersion, 10))
	c.authorize(req)

	slog.Debug("store delete", "namespace", namespace, "key", key, "expectedVersion", expectedVersion)

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
		return c.statusError("delete", resp)
	}
}

// authorize attaches the bearer token when one is configured.
func (c *httpStore) authorize(req *http.Request) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// statusError builds an error from an unexpected response, including a short snippet
// of the body for context.
func (c *httpStore) statusError(op string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(body))
	if msg == "" {
		return fmt.Errorf("kv %s: unexpected status %s", op, resp.Status)
	}
	return fmt.Errorf("kv %s: unexpected status %s: %s", op, resp.Status, msg)
}

// parseVersion reads a version header, treating a missing or malformed value as 0.
func parseVersion(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// drainClose drains and closes a response body so the connection can be reused.
func drainClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
