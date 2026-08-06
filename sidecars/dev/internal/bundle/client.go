package bundle

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// requestTimeout bounds a single call to the orchestrator. Generous relative to
	// the work (one row plus its resources) but finite, so a wedged connection cannot
	// hold the reload lock open forever.
	requestTimeout = 30 * time.Second
	// maxBundleBytes caps how much the sidecar will read into memory. An integration
	// definition plus its resources is kilobytes; this is the backstop against a
	// response that never ends.
	maxBundleBytes = 16 << 20 // 16 MiB
)

// Client talks to the orchestrator on behalf of one dev run. It carries the
// dev-run token, which authorises exactly this run's bundle and nothing else —
// not a user credential, so a compromised dev pod reaches no further than the
// integration it was already running.
type Client struct {
	baseURL  string
	devRunID string
	token    string
	http     *http.Client
}

// NewClient returns a Client for devRunID against the orchestrator at baseURL.
func NewClient(baseURL, devRunID, token string) *Client {
	return &Client{
		baseURL:  strings.TrimSuffix(baseURL, "/"),
		devRunID: devRunID,
		token:    token,
		http:     &http.Client{Timeout: requestTimeout},
	}
}

// Fetch retrieves this dev run's workspace bundle.
//
// The error it returns is the input to a decision the caller has to make, so the
// mapping is explicit rather than incidental: 404 is {@link ErrGone} (terminal),
// 401/403 is ErrUnauthorized (retry, loudly), and everything else — including any
// 5xx and any transport failure — is an ordinary error the caller should retry.
func (c *Client) Fetch(ctx context.Context) (Bundle, error) {
	req, err := c.newRequest(ctx, http.MethodGet, "bundle")
	if err != nil {
		return Bundle{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return Bundle{}, fmt.Errorf("fetch bundle: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if err := statusError(resp, "fetch bundle"); err != nil {
		return Bundle{}, err
	}

	var b Bundle
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBundleBytes)).Decode(&b); err != nil {
		return Bundle{}, fmt.Errorf("decode bundle: %w", err)
	}
	return b, nil
}

// Expire tells the orchestrator to tear this dev run down. Called when Fetch
// reports ErrGone: the sidecar cannot delete its own workload (it holds no
// Kubernetes credential, by design), so it asks the one peer it has.
//
// A separate endpoint from the user-facing DELETE /devruns/{id} on purpose — that
// route authorises a user action, and a pod holding a dev-run token is not a user.
func (c *Client) Expire(ctx context.Context) error {
	req, err := c.newRequest(ctx, http.MethodPost, "expire")
	if err != nil {
		return err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("expire dev run: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	// Drain so the connection can be reused; the body carries nothing we need.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxBundleBytes))

	// A 404 here means the run is already gone, which is the outcome being asked
	// for. Reporting it as an error would make the caller retry a request that has
	// already succeeded in every sense that matters.
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return statusError(resp, "expire dev run")
}

// newRequest builds an authenticated request for one of this dev run's subpaths.
func (c *Client) newRequest(ctx context.Context, method, action string) (*http.Request, error) {
	target := fmt.Sprintf("%s/devruns/%s/%s", c.baseURL, url.PathEscape(c.devRunID), action)
	req, err := http.NewRequestWithContext(ctx, method, target, nil)
	if err != nil {
		return nil, fmt.Errorf("build %s request: %w", action, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return req, nil
}

// statusError maps a non-2xx response to the sentinel the caller branches on, or
// to a plain error carrying the status and a bounded snippet of the body — the
// body is where the orchestrator explains itself, and dropping it would leave a
// bare status code as the only clue.
func statusError(resp *http.Response, op string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return ErrGone
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUnauthorized
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	msg := strings.TrimSpace(string(snippet))
	if msg == "" {
		return fmt.Errorf("%s: unexpected status %s", op, resp.Status)
	}
	return fmt.Errorf("%s: unexpected status %s: %s", op, resp.Status, msg)
}
