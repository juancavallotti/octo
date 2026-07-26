package observability

import (
	"context"
	"flag"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/services"
)

// newTestService returns a started service bound to an OS-assigned port, plus the
// health gate driving its readiness. It is stopped on cleanup.
func newTestService(t *testing.T) (*Service, *services.Health) {
	t.Helper()
	t.Setenv(envEnabled, "false")
	t.Setenv(envMetrics, "false")

	svc := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	svc.Flags(fs)
	// Port 0 lets the OS pick, so tests never collide with each other or with
	// whatever is already on 39999.
	if err := fs.Parse([]string{"--observability=true", "--observability-addr", "127.0.0.1:0"}); err != nil {
		t.Fatalf("parse: %v", err)
	}

	health := services.NewHealth()
	if err := svc.Start(context.Background(), health); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := svc.Stop(context.Background()); err != nil {
			t.Errorf("Stop: %v", err)
		}
	})
	return svc, health
}

// get fetches a path from the service's admin port.
func get(t *testing.T, svc *Service, path string) (int, string) {
	t.Helper()
	resp, err := http.Get("http://" + svc.Addr() + path) //nolint:noctx // short-lived test request
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return resp.StatusCode, strings.TrimSpace(string(body))
}

// Liveness is unconditional: the handler running at all is the answer.
func TestHealthzAlwaysOK(t *testing.T) {
	svc, health := newTestService(t)

	for _, state := range []services.State{
		services.StateStarting, services.StateReady,
		services.StateReloading, services.StateDraining, services.StateStopped,
	} {
		health.SetState(state)
		if status, body := get(t, svc, pathHealthz); status != http.StatusOK || body != "ok" {
			t.Errorf("in state %q, GET /healthz = %d %q, want 200 \"ok\"", state, status, body)
		}
	}
}

// Readiness is 200 only when serving, and names the reason otherwise — "why is
// this pod not ready" is the next question after the 503.
func TestReadyzReflectsState(t *testing.T) {
	tests := []struct {
		state      services.State
		wantStatus int
	}{
		{services.StateStarting, http.StatusServiceUnavailable},
		{services.StateReady, http.StatusOK},
		{services.StateReloading, http.StatusServiceUnavailable},
		{services.StateDraining, http.StatusServiceUnavailable},
		{services.StateStopped, http.StatusServiceUnavailable},
	}

	svc, health := newTestService(t)
	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			health.SetState(tt.state)
			status, body := get(t, svc, pathReadyz)
			if status != tt.wantStatus {
				t.Errorf("GET /readyz = %d, want %d", status, tt.wantStatus)
			}
			if body != string(tt.state) {
				t.Errorf("GET /readyz body = %q, want the state %q", body, tt.state)
			}
		})
	}
}

// A probe answering during startup is the point of having one: before the CLI has
// reported anything, readiness must already be refusing rather than the port
// refusing the connection.
func TestReadyzAnswersBeforeAnythingIsReady(t *testing.T) {
	svc, _ := newTestService(t)

	status, body := get(t, svc, pathReadyz)
	if status != http.StatusServiceUnavailable {
		t.Fatalf("GET /readyz on a fresh process = %d, want 503", status)
	}
	if body != string(services.StateStarting) {
		t.Errorf("body = %q, want %q", body, services.StateStarting)
	}
}

func TestIndexListsEndpointsAndUnknownPaths404(t *testing.T) {
	svc, _ := newTestService(t)

	status, body := get(t, svc, "/")
	if status != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", status)
	}
	for _, want := range []string{pathHealthz, pathReadyz} {
		if !strings.Contains(body, want) {
			t.Errorf("index does not mention %s:\n%s", want, body)
		}
	}

	if status, _ := get(t, svc, "/nope"); status != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404 — the index must not answer for every path", status)
	}
}

// Probes are polled constantly and their answer changes with the runtime's state.
func TestProbesAreNotCacheable(t *testing.T) {
	svc, _ := newTestService(t)

	resp, err := http.Get("http://" + svc.Addr() + pathReadyz) //nolint:noctx // short-lived test request
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if got := resp.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want %q", got, "no-store")
	}
}

// --observability=false binds nothing at all.
func TestDisabledBindsNothing(t *testing.T) {
	svc := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	svc.Flags(fs)
	if err := fs.Parse([]string{"--observability=false"}); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if err := svc.Start(context.Background(), services.NewHealth()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if addr := svc.Addr(); addr != "" {
		t.Errorf("disabled service bound %s, want nothing", addr)
	}
	// Stop must be safe on a service that never started.
	if err := svc.Stop(context.Background()); err != nil {
		t.Errorf("Stop on a disabled service: %v", err)
	}
}

// A port conflict fails the run rather than becoming a log line nobody reads.
func TestStartFailsOnPortConflict(t *testing.T) {
	first, _ := newTestService(t)

	second := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	second.Flags(fs)
	if err := fs.Parse([]string{"--observability=true", "--observability-addr", first.Addr()}); err != nil {
		t.Fatalf("parse: %v", err)
	}

	err := second.Start(context.Background(), services.NewHealth())
	if err == nil {
		t.Fatal("Start on a taken port = nil, want an error")
	}
	if !strings.Contains(err.Error(), first.Addr()) {
		t.Errorf("error %q does not name the address it could not bind", err)
	}
}

func TestFlagsDefaultToEnvironment(t *testing.T) {
	t.Setenv(envAddr, "127.0.0.1:12345")
	t.Setenv(envEnabled, "false")

	svc := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	svc.Flags(fs)
	if err := fs.Parse(nil); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if svc.addr != "127.0.0.1:12345" {
		t.Errorf("addr = %q, want the env value", svc.addr)
	}
	if svc.enabled {
		t.Error("enabled = true, want the env value false")
	}
}

// A passed flag beats the environment, which falls out of the defaults being
// env-resolved rather than from precedence code.
func TestFlagBeatsEnvironment(t *testing.T) {
	t.Setenv(envAddr, "127.0.0.1:12345")

	svc := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	svc.Flags(fs)
	if err := fs.Parse([]string{"--observability-addr", "127.0.0.1:54321"}); err != nil {
		t.Fatalf("parse: %v", err)
	}

	if svc.addr != "127.0.0.1:54321" {
		t.Errorf("addr = %q, want the flag to win over the env", svc.addr)
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		set      bool
		fallback bool
		want     bool
	}{
		{name: "unset falls back", fallback: true, want: true},
		{name: "empty falls back", value: "", set: true, fallback: true, want: true},
		{name: "false", value: "false", set: true, fallback: true, want: false},
		{name: "0", value: "0", set: true, fallback: true, want: false},
		{name: "off", value: "off", set: true, fallback: true, want: false},
		{name: "no", value: "no", set: true, fallback: true, want: false},
		{name: "OFF is case-insensitive", value: "OFF", set: true, fallback: true, want: false},
		{name: "true", value: "true", set: true, fallback: false, want: true},
		{name: "on", value: "on", set: true, fallback: false, want: true},
		{name: "yes", value: "yes", set: true, fallback: false, want: true},
		{name: "whitespace is trimmed", value: "  off  ", set: true, fallback: true, want: false},
		{name: "garbage falls back rather than failing", value: "maybe", set: true, fallback: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const name = "OCTO_TEST_BOOL"
			if tt.set {
				t.Setenv(name, tt.value)
			}
			if got := envBool(name, tt.fallback); got != tt.want {
				t.Errorf("envBool(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// The service registers itself, so a binary blank-importing this package gets it
// without any wiring of its own.
func TestServiceRegistersItself(t *testing.T) {
	for _, service := range services.Hosted() {
		if service.Name() == serviceName {
			return
		}
	}
	t.Fatalf("%q is not registered; a blank import must be enough to wire it", serviceName)
}

// A flag absent from --help may as well not exist, so every flag the service
// registers has to appear in the section it contributes.
func TestUsageDocumentsEveryFlag(t *testing.T) {
	svc := New()
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	svc.Flags(fs)

	usage := svc.Usage()
	fs.VisitAll(func(f *flag.Flag) {
		if !strings.Contains(usage, "--"+f.Name) {
			t.Errorf("flag --%s is not documented in Usage()", f.Name)
		}
	})
}
