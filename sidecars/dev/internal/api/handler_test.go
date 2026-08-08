package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/sidecars/dev/internal/reload"
	"github.com/juancavallotti/octo/sidecars/dev/internal/runtimeprobe"
)

const testToken = "sidecar-token"

// fakeReloader stands in for the coordinator.
type fakeReloader struct {
	calls   int
	outcome reload.Outcome
	err     error
	state   reload.State
}

func (f *fakeReloader) Reload(context.Context) (reload.Outcome, error) {
	f.calls++
	return f.outcome, f.err
}

func (f *fakeReloader) State() reload.State { return f.state }

// fakeWorkspace stands in for the managed directory.
type fakeWorkspace struct {
	dir       string
	files     []string
	listErr   error
	removed   int
	removeErr error
}

func (f *fakeWorkspace) Dir() string             { return f.dir }
func (f *fakeWorkspace) List() ([]string, error) { return f.files, f.listErr }
func (f *fakeWorkspace) RemoveConfig() error     { f.removed++; return f.removeErr }

// fakeProber stands in for the runtime beside us.
type fakeProber struct {
	addr    string
	status  runtimeprobe.Status
	metrics []byte
	ctype   string
	err     error
}

func (f *fakeProber) Addr() string                               { return f.addr }
func (f *fakeProber) Status(context.Context) runtimeprobe.Status { return f.status }
func (f *fakeProber) Metrics(context.Context) ([]byte, string, error) {
	return f.metrics, f.ctype, f.err
}

// newTestServer wires the handler through a REAL http.ServeMux, so the route
// patterns and method matching are part of what is tested rather than bypassed.
func newTestServer(r reloader, ws workspaceFS, p prober) *httptest.Server {
	mux := http.NewServeMux()
	NewHandler(r, ws, p, testToken).Register(mux)
	return httptest.NewServer(mux)
}

// do issues a request with an optional Authorization header.
func do(t *testing.T, srv *httptest.Server, method, path, auth string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return resp
}

// decode reads a JSON body into dst.
func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer func() { _ = resp.Body.Close() }()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// Every command route is behind the token, and the probes are in front of it. This
// is the whole authorisation model, so it is asserted route by route rather than
// sampled — a route accidentally registered outside the wrapper is exactly the bug
// that would not show up anywhere else.
func TestAuthCoversEveryCommandRoute(t *testing.T) {
	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/reload"},
		{http.MethodGet, "/status"},
		{http.MethodGet, "/diagnostics"},
		{http.MethodGet, "/metrics"},
		{http.MethodDelete, "/workspace/config"},
	}
	auths := []struct {
		name string
		auth string
	}{
		{"missing", ""},
		{"wrong token", "Bearer nope"},
		{"empty token", "Bearer "},
		{"wrong scheme", "Basic " + testToken},
		{"bare token", testToken},
	}

	for _, route := range routes {
		for _, a := range auths {
			t.Run(route.method+" "+route.path+" "+a.name, func(t *testing.T) {
				srv := newTestServer(&fakeReloader{}, &fakeWorkspace{}, &fakeProber{})
				defer srv.Close()

				resp := do(t, srv, route.method, route.path, a.auth)
				defer func() { _ = resp.Body.Close() }()
				if resp.StatusCode != http.StatusUnauthorized {
					t.Errorf("status = %d, want 401", resp.StatusCode)
				}
			})
		}
	}
}

// The scheme is matched case-insensitively, because "bearer" is what some clients
// send and rejecting it would be a confusing 401.
func TestAuthAcceptsLowercaseScheme(t *testing.T) {
	srv := newTestServer(&fakeReloader{}, &fakeWorkspace{}, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/status", "bearer "+testToken)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// Probes must answer without a token: a kubelet probe cannot carry one.
func TestProbesNeedNoToken(t *testing.T) {
	srv := newTestServer(&fakeReloader{state: reload.State{Pulls: 1}}, &fakeWorkspace{}, &fakeProber{})
	defer srv.Close()

	for _, path := range []string{"/healthz", "/readyz"} {
		resp := do(t, srv, http.MethodGet, path, "")
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s status = %d, want 200", path, resp.StatusCode)
		}
	}
}

// Readiness is what orders pod startup: the runtime container must not start until
// the workspace has been populated once.
func TestReadyzGatesOnFirstPull(t *testing.T) {
	tests := []struct {
		name  string
		state reload.State
		want  int
	}{
		{"before the first pull", reload.State{}, http.StatusServiceUnavailable},
		{"first pull failing", reload.State{LastError: "boom"}, http.StatusServiceUnavailable},
		{"after the first pull", reload.State{Pulls: 1}, http.StatusOK},
		{"expired", reload.State{Pulls: 3, Expired: true}, http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(&fakeReloader{state: tc.state}, &fakeWorkspace{}, &fakeProber{})
			defer srv.Close()

			resp := do(t, srv, http.MethodGet, "/readyz", "")
			_ = resp.Body.Close()
			if resp.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

func TestReloadApplied(t *testing.T) {
	r := &fakeReloader{outcome: reload.OutcomeApplied, state: reload.State{Pulls: 1, Generation: "g4"}}
	srv := newTestServer(r, &fakeWorkspace{}, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodPost, "/reload", "Bearer "+testToken)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got reload.State
	decode(t, resp, &got)
	if got.Generation != "g4" {
		t.Errorf("generation = %q, want g4", got.Generation)
	}
	if r.calls != 1 {
		t.Errorf("Reload calls = %d, want 1", r.calls)
	}
}

// A coalesced reload is 202, not 200: the work is guaranteed but has not happened
// yet, and claiming 200 would say the workspace is current when it may not be.
func TestReloadCoalescedIsAccepted(t *testing.T) {
	srv := newTestServer(&fakeReloader{outcome: reload.OutcomeCoalesced}, &fakeWorkspace{}, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodPost, "/reload", "Bearer "+testToken)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want 202", resp.StatusCode)
	}
}

// 503 rather than 500: every way a reload fails is a dependency being unavailable.
func TestReloadFailureIsUnavailable(t *testing.T) {
	srv := newTestServer(&fakeReloader{err: errors.New("orchestrator unreachable")}, &fakeWorkspace{}, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodPost, "/reload", "Bearer "+testToken)
	if resp.StatusCode != http.StatusServiceUnavailable {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
	var body map[string]string
	decode(t, resp, &body)
	if body["error"] == "" {
		t.Error("error body is empty, want the failure explained")
	}
}

func TestStatusReportsBothHalves(t *testing.T) {
	r := &fakeReloader{state: reload.State{Pulls: 2, Generation: "g9"}}
	ws := &fakeWorkspace{dir: "/workspace/integrations"}
	p := &fakeProber{status: runtimeprobe.Status{Reachable: true, Live: true, Ready: true, State: "ready"}}
	srv := newTestServer(r, ws, p)
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/status", "Bearer "+testToken)
	var got statusResponse
	decode(t, resp, &got)

	if got.Workspace != "/workspace/integrations" {
		t.Errorf("workspace = %q", got.Workspace)
	}
	if got.Reload.Generation != "g9" || got.Reload.Pulls != 2 {
		t.Errorf("reload = %+v", got.Reload)
	}
	if !got.Runtime.Ready || got.Runtime.State != "ready" {
		t.Errorf("runtime = %+v", got.Runtime)
	}
}

// The runtime being unreachable must NOT fail /status. During startup it is the
// expected state, and a 500 here would hide the one thing the caller asked about.
func TestStatusSurvivesUnreachableRuntime(t *testing.T) {
	p := &fakeProber{status: runtimeprobe.Status{Error: "connection refused"}}
	srv := newTestServer(&fakeReloader{}, &fakeWorkspace{}, p)
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/status", "Bearer "+testToken)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got statusResponse
	decode(t, resp, &got)
	if got.Runtime.Reachable {
		t.Error("Runtime.Reachable = true, want false")
	}
	if got.Runtime.Error == "" {
		t.Error("Runtime.Error is empty, want the reason reported")
	}
}

func TestDiagnosticsListsFiles(t *testing.T) {
	ws := &fakeWorkspace{dir: "/w", files: []string{".env.dev", "integration.yaml"}}
	p := &fakeProber{addr: "127.0.0.1:39999"}
	srv := newTestServer(&fakeReloader{}, ws, p)
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/diagnostics", "Bearer "+testToken)
	var got diagnosticsResponse
	decode(t, resp, &got)

	if len(got.Files) != 2 {
		t.Errorf("files = %v, want two", got.Files)
	}
	if got.RuntimeAdmin != "127.0.0.1:39999" {
		t.Errorf("runtimeAdmin = %q", got.RuntimeAdmin)
	}
	if got.FilesError != "" {
		t.Errorf("filesError = %q, want empty", got.FilesError)
	}
}

// A listing failure is reported inline. Losing the whole diagnostic because one
// part of it failed is the opposite of useful when debugging.
func TestDiagnosticsReportsListErrorInline(t *testing.T) {
	ws := &fakeWorkspace{dir: "/w", listErr: errors.New("permission denied")}
	srv := newTestServer(&fakeReloader{}, ws, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/diagnostics", "Bearer "+testToken)
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got diagnosticsResponse
	decode(t, resp, &got)
	if got.FilesError == "" {
		t.Error("filesError is empty, want the listing failure reported")
	}
}

func TestMetricsPassthrough(t *testing.T) {
	p := &fakeProber{metrics: []byte("octo_up 1\n"), ctype: "text/plain; version=0.0.4"}
	srv := newTestServer(&fakeReloader{}, &fakeWorkspace{}, p)
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/metrics", "Bearer "+testToken)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/plain; version=0.0.4" {
		t.Errorf("Content-Type = %q", got)
	}
}

func TestMetricsUnavailable(t *testing.T) {
	p := &fakeProber{err: errors.New("not served")}
	srv := newTestServer(&fakeReloader{}, &fakeWorkspace{}, p)
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/metrics", "Bearer "+testToken)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

func TestRemoveConfig(t *testing.T) {
	ws := &fakeWorkspace{}
	srv := newTestServer(&fakeReloader{}, ws, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodDelete, "/workspace/config", "Bearer "+testToken)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	if ws.removed != 1 {
		t.Errorf("RemoveConfig calls = %d, want 1", ws.removed)
	}
}

func TestRemoveConfigFailure(t *testing.T) {
	ws := &fakeWorkspace{removeErr: errors.New("read-only volume")}
	srv := newTestServer(&fakeReloader{}, ws, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodDelete, "/workspace/config", "Bearer "+testToken)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
}

// Method matching comes from the mux patterns, so a wrong method must not reach a
// handler — and must not be answered as an auth failure either, which would hide
// the real mistake.
func TestWrongMethodIsRejected(t *testing.T) {
	srv := newTestServer(&fakeReloader{}, &fakeWorkspace{}, &fakeProber{})
	defer srv.Close()

	resp := do(t, srv, http.MethodGet, "/reload", "Bearer "+testToken)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", resp.StatusCode)
	}
}

// Endpoints() feeds the startup log, which is this API's only documentation, so it
// has to stay in step with what Register actually mounts.
func TestEndpointsMatchRegisteredRoutes(t *testing.T) {
	srv := newTestServer(&fakeReloader{state: reload.State{Pulls: 1}}, &fakeWorkspace{}, &fakeProber{})
	defer srv.Close()

	for _, endpoint := range Endpoints() {
		method, path, ok := splitEndpoint(endpoint)
		if !ok {
			t.Fatalf("endpoint %q is not %q-shaped", endpoint, "METHOD /path")
		}
		resp := do(t, srv, method, path, "Bearer "+testToken)
		_ = resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
			t.Errorf("%s is listed but not routed (status %d)", endpoint, resp.StatusCode)
		}
	}
}

// splitEndpoint splits "GET /path" into its method and path.
func splitEndpoint(endpoint string) (method, path string, ok bool) {
	for i := range len(endpoint) {
		if endpoint[i] == ' ' {
			return endpoint[:i], endpoint[i+1:], true
		}
	}
	return "", "", false
}
