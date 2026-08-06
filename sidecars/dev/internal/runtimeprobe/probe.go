// Package runtimeprobe reads the octo runtime's admin endpoints over loopback.
//
// The two containers share a network namespace, so the runtime's admin port is
// reachable at 127.0.0.1 and needs no service, no credential and no exposure. What
// it offers is documented at runtime/services/observability/observability.go:119 —
// GET /healthz is unconditional liveness, GET /readyz answers 200 with the state
// name once every connector and flow of the current generation started and 503
// with the state name otherwise, and GET /metrics is Prometheus exposition when
// the runtime was started with metrics enabled.
//
// /readyz putting the reason in its body is what makes this package worth having:
// "why is this pod not ready" is the next question after "is it ready", and the
// runtime already answers both in one call.
package runtimeprobe

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	// probeTimeout bounds a probe. Short on purpose: this is loopback, and a
	// diagnostic that hangs is worse than one that reports a timeout.
	probeTimeout = 3 * time.Second
	// maxBodyBytes bounds what a probe reads. /readyz answers with a state name;
	// /metrics is larger, and has its own limit at the call site.
	maxBodyBytes = 4 << 10
	// maxMetricsBytes bounds the metrics passthrough.
	maxMetricsBytes = 4 << 20
)

// Status is what the sidecar can say about the runtime beside it.
//
// Reachable is reported separately from Live because they answer different
// questions and conflating them loses the useful one: an unreachable admin port
// during startup is expected, while an unreachable one after the runtime has been
// serving means it died.
type Status struct {
	Reachable bool `json:"reachable"`
	Live      bool `json:"live"`
	Ready     bool `json:"ready"`
	// State is the runtime's own word for what it is doing, taken verbatim from
	// /readyz's body: starting, ready, reloading, and so on.
	State string `json:"state,omitempty"`
	// Error explains a failed probe. Present only when Reachable is false.
	Error string `json:"error,omitempty"`
}

// Prober reads one runtime's admin port.
type Prober struct {
	addr string
	http *http.Client
}

// New returns a Prober for the admin address addr, e.g. "127.0.0.1:39999".
func New(addr string) *Prober {
	return &Prober{addr: addr, http: &http.Client{Timeout: probeTimeout}}
}

// Addr returns the admin address being probed.
func (p *Prober) Addr() string { return p.addr }

// Status probes liveness and readiness.
//
// It returns no error, and that is the contract rather than an omission: an
// unreachable runtime is an answer to the question being asked, not a failure to
// answer it. A caller rendering /status wants "not reachable, here is why", never
// a 500 that says nothing about the runtime at all.
func (p *Prober) Status(ctx context.Context) Status {
	if _, err := p.get(ctx, "/healthz"); err != nil {
		return Status{Error: err.Error()}
	}
	// Liveness is unconditional in the runtime, so reaching it at all is the answer.
	out := Status{Reachable: true, Live: true}

	code, body, err := p.getWithStatus(ctx, "/readyz")
	if err != nil {
		// Live but readiness unreadable: report what is known rather than discarding
		// the liveness result that just succeeded.
		out.Error = err.Error()
		return out
	}
	out.State = strings.TrimSpace(body)
	out.Ready = code == http.StatusOK
	return out
}

// Metrics returns the runtime's Prometheus exposition verbatim, with its content
// type. Passed through rather than parsed: the sidecar has no opinion about the
// metrics, and re-serialising them would only be a chance to corrupt them.
func (p *Prober) Metrics(ctx context.Context) (body []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url("/metrics"), nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("probe /metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxMetricsBytes))
	if err != nil {
		return nil, "", fmt.Errorf("read /metrics: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 404 here is the ordinary case of a runtime started without --metrics, so the
		// status is reported rather than dressed up as a transport failure.
		return nil, "", fmt.Errorf("probe /metrics: status %s", resp.Status)
	}
	return raw, resp.Header.Get("Content-Type"), nil
}

// get reads a probe path, treating any non-2xx as an error.
func (p *Prober) get(ctx context.Context, path string) (string, error) {
	code, body, err := p.getWithStatus(ctx, path)
	if err != nil {
		return "", err
	}
	if code < 200 || code >= 300 {
		return body, fmt.Errorf("probe %s: status %d", path, code)
	}
	return body, nil
}

// getWithStatus reads a probe path, returning the status code and body. A non-2xx
// is not an error here: /readyz answers 503 in normal operation, and its body is
// the reason worth reporting.
func (p *Prober) getWithStatus(ctx context.Context, path string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.url(path), nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return 0, "", fmt.Errorf("probe %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return resp.StatusCode, "", fmt.Errorf("read %s: %w", path, err)
	}
	return resp.StatusCode, string(raw), nil
}

// url builds an admin URL for path.
func (p *Prober) url(path string) string {
	return "http://" + p.addr + path
}
