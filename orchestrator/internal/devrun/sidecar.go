package devrun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	// commandTimeout bounds a reload command. Generous enough for the sidecar to be
	// mid-pull (it serialises them, so a command can wait on one) and finite so an
	// unreachable pod cannot hold a save-path notification open.
	commandTimeout = 10 * time.Second
	// statusTimeout is shorter, because status is polled and is decoration: the
	// cluster already answered the question that matters ("is it running?"), so a
	// sidecar that does not reply promptly is simply left out of the answer.
	statusTimeout = 2 * time.Second
	// maxSidecarBody caps what is read back from a sidecar. Its status response is a
	// few hundred bytes; this is the backstop against one that never ends.
	maxSidecarBody = 1 << 20
)

// httpSidecar commands dev-run sidecars over HTTP. One instance serves every dev
// run — the per-run parts (address and bearer) are arguments, because both are
// derived per call rather than held.
type httpSidecar struct {
	client *http.Client
}

// newSidecarClient returns the sidecar commander used in production.
func newSidecarClient() *httpSidecar {
	// No global Timeout: the two operations want different bounds and each passes its
	// own through the request context.
	return &httpSidecar{client: &http.Client{}}
}

// Reload tells the sidecar to pull its bundle and apply it. The command carries no
// payload at all — the sidecar reads the definition and resources from the
// orchestrator itself, so this only says "go look again".
func (s *httpSidecar) Reload(ctx context.Context, baseURL, token string) error {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	_, err := s.do(ctx, http.MethodPost, baseURL+"/reload", token)
	return err
}

// Status returns the sidecar's own view of the run — which generation it applied,
// when it last pulled, what the runtime's admin port says — as the opaque JSON the
// sidecar produced.
//
// Opaque on purpose. Re-declaring the sidecar's status schema here would create two
// copies of it in two modules that version independently, and the orchestrator
// interprets none of it: this travels through to the editor's diagnostics view.
func (s *httpSidecar) Status(ctx context.Context, baseURL, token string) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	body, err := s.do(ctx, http.MethodGet, baseURL+"/status", token)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("%w: status response is not json", ErrSidecarUnreachable)
	}
	return body, nil
}

// do performs one authenticated request and returns the body, mapping any failure
// to ErrSidecarUnreachable so callers branch on one thing.
//
// A transport error and a 5xx are the same fact for every caller here — the pod is
// not answering usefully — and the distinction that does matter (does the dev run
// exist at all?) was already settled by the cluster listing before this was called.
func (s *httpSidecar) do(ctx context.Context, method, url, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("devrun: build sidecar request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSidecarUnreachable, url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxSidecarBody))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: %s: %s", ErrSidecarUnreachable, url, resp.Status)
	}
	if readErr != nil {
		return nil, fmt.Errorf("%w: %s: %w", ErrSidecarUnreachable, url, readErr)
	}
	return body, nil
}
