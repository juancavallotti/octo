package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// startConnector starts a connector bound to an ephemeral loopback port and
// registers cleanup. It returns the connector and its base URL.
func startConnector(t *testing.T, settings map[string]any) (*Connector, string) {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	settings["host"] = "127.0.0.1"
	settings["port"] = 0

	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c, "http://" + c.ln.Addr().String()
}

// newSource builds a source on c with the given settings, starts it, and returns
// its output channel.
func newSource(t *testing.T, c *Connector, settings map[string]any) chan *types.Message {
	t.Helper()
	out := make(chan *types.Message, 1)
	src, err := c.NewSource(types.SourceConfig{Type: "http", Settings: settings}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	if err := src.Start(context.Background()); err != nil {
		t.Fatalf("source Start: %v", err)
	}
	t.Cleanup(func() { _ = src.Stop(context.Background()) })
	return out
}

// echoWorker reads one message, applies fn (which sets the terminal outcome on
// the returned event), and publishes it so the parked handler can respond.
func echoWorker(out <-chan *types.Message, fn func(msg *types.Message) types.FlowEvent) {
	go func() {
		msg, ok := <-out
		if !ok {
			return
		}
		ev := fn(msg)
		ev.EventID = msg.EventID
		ev.OccurredAt = time.Now()
		core.DefaultEventBus().Publish(ev)
	}()
}

func TestRequestResponseCompleted(t *testing.T) {
	c, base := startConnector(t, map[string]any{"basePath": "/api/v1"})
	out := newSource(t, c, map[string]any{
		"path":                "/orders/{id}",
		"headers":             []string{"X-Tenant"},
		"correlationIdHeader": "X-Request-Id",
	})

	// Echo the variables back as the body so we can assert what the source set.
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = map[string]any(msg.Variables)
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/api/v1/orders/42?currency=EUR", bytes.NewReader([]byte(`{"item":"widget"}`)))
	req.Header.Set("X-Tenant", "acme")
	req.Header.Set("X-Request-Id", "r1")

	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	var vars map[string]any
	if err := json.Unmarshal(resp.body, &vars); err != nil {
		t.Fatalf("decode body: %v (%s)", err, resp.body)
	}
	if vars["id"] != "42" {
		t.Errorf("vars.id = %v, want 42", vars["id"])
	}
	if vars["method"] != http.MethodPost {
		t.Errorf("vars.method = %v, want POST", vars["method"])
	}
	if vars["X-Tenant"] != "acme" {
		t.Errorf("vars[X-Tenant] = %v, want acme", vars["X-Tenant"])
	}
	query, ok := vars["query"].(map[string]any)
	if !ok || query["currency"] != "EUR" {
		t.Errorf("vars.query = %v, want currency=EUR", vars["query"])
	}
}

func TestRawBodyVarCaptured(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, map[string]any{
		"path":       "/ingest",
		"rawBodyVar": "rawBody",
	})

	// Echo the variables back so we can assert the raw body was captured verbatim.
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = map[string]any(msg.Variables)
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	// A body with insignificant whitespace: SetBodyJSON would re-serialize it, but
	// the raw var must hold the exact bytes sent.
	raw := "{\n  \"item\" : \"widget\"\n}"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/ingest", bytes.NewReader([]byte(raw)))
	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	var vars map[string]any
	if err := json.Unmarshal(resp.body, &vars); err != nil {
		t.Fatalf("decode body: %v (%s)", err, resp.body)
	}
	if vars["rawBody"] != raw {
		t.Errorf("vars.rawBody = %q, want %q", vars["rawBody"], raw)
	}
}

func TestRawContentResponse(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, map[string]any{"path": "/page"})

	// The flow produces a raw HTML body; the source must serve it verbatim with
	// the given Content-Type rather than JSON-encoding it.
	const html = "<h1>hello</h1>"
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.SetRawBody("text/html; charset=utf-8", html)
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/page", nil)
	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	if got := string(resp.body); got != html {
		t.Errorf("body = %q, want %q", got, html)
	}
	if ct := resp.header.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
	}
}

func TestRawBodySourced(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, map[string]any{
		"path":    "/ingest",
		"rawBody": true,
	})

	// Echo the raw body back so we can assert it was captured with its content type.
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		ct, data, ok := msg.RawBody()
		msg.Body = map[string]any{"contentType": ct, "rawData": data, "ok": ok}
		msg.RawContent = false
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	// A non-JSON body that the default (JSON-gated) source would reject with 400.
	raw := "name=Ann&count=2"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		base+"/ingest", bytes.NewReader([]byte(raw)))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	var got map[string]any
	if err := json.Unmarshal(resp.body, &got); err != nil {
		t.Fatalf("decode body: %v (%s)", err, resp.body)
	}
	if got["ok"] != true {
		t.Fatalf("RawBody ok = %v, want true", got["ok"])
	}
	if got["contentType"] != "application/x-www-form-urlencoded" {
		t.Errorf("contentType = %v, want application/x-www-form-urlencoded", got["contentType"])
	}
	if got["rawData"] != raw {
		t.Errorf("rawData = %v, want %q", got["rawData"], raw)
	}
}

// TestCompletedNilBodyIsEmpty verifies that a flow completing with no body
// produces an empty response rather than the literal "null".
func TestCompletedNilBodyIsEmpty(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, map[string]any{"path": "/noop"})
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = nil
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/noop", nil)
	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	if len(resp.body) != 0 {
		t.Errorf("body = %q, want empty", resp.body)
	}
}

func TestRequestResponseOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		event      types.FlowEventKind
		err        error
		publish    bool
		wantStatus int
	}{
		{name: "completed", event: types.FlowEventCompleted, publish: true, wantStatus: http.StatusOK},
		{name: "dropped", event: types.FlowEventDropped, publish: true, wantStatus: http.StatusNoContent},
		{name: "failed", event: types.FlowEventFailed, publish: true, wantStatus: http.StatusInternalServerError},
		{name: "timeout", publish: false, wantStatus: http.StatusGatewayTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, base := startConnector(t, nil)
			out := newSource(t, c, map[string]any{
				"path":    "/run/" + tt.name,
				"timeout": "200ms",
			})
			if tt.publish {
				echoWorker(out, func(msg *types.Message) types.FlowEvent {
					return types.FlowEvent{Kind: tt.event, Result: msg, Err: tt.err}
				})
			} else {
				// Drain the message but never publish, to force a timeout.
				go func() { <-out }()
			}

			req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/run/"+tt.name, nil)
			resp := do(t, req)
			if resp.status != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", resp.status, tt.wantStatus, resp.body)
			}
		})
	}
}

func TestMalformedBodyRejected(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, map[string]any{"path": "/ingest"})
	// No worker: a malformed body must be rejected before any message is sent.
	go func() {
		if msg, ok := <-out; ok {
			t.Errorf("unexpected message sent for malformed body: %v", msg)
		}
	}()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, base+"/ingest", bytes.NewReader([]byte(`{bad`)))
	resp := do(t, req)
	if resp.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", resp.status, resp.body)
	}
}

// TestStopReleasesPortWithoutServing reproduces the hot-reload "address already
// in use" leak: a connector binds its port in Start but defers serving until a
// source calls ensureServing. If the config reload fails before any request, the
// listener must still be released by Stop so the next generation can re-bind the
// same port.
func TestStopReleasesPortWithoutServing(t *testing.T) {
	port := freePort(t)
	settings := map[string]any{"host": "127.0.0.1", "port": port}

	first := &Connector{}
	if err := first.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	// Stop without ever serving — mirrors a failed reload after connectors start.
	if err := first.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// Stop is idempotent, and the second call must not block waiting for an accept
	// loop that the first call made sure would never run.
	if err := first.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop on the same connector: %v", err)
	}

	second := &Connector{}
	if err := second.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("re-bind %d after Stop: %v", port, err)
	}
	if err := second.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// TestStopAfterServingReturnsNoError pins the exit status of a clean shutdown.
// Stop used to close the listener itself even when Serve had taken it, racing
// the accept loop's untrack; whenever server.Shutdown won that race it closed
// the same listener a second time and reported "use of closed network
// connection", turning a clean SIGTERM into a non-zero exit that Kubernetes and
// systemd read as a crash. The race makes the old failure intermittent, so this
// asserts the contract rather than reproducing the losing schedule: a connector
// that served must report no error from Stop. No other test would notice — they
// all discard the error their cleanup Stop returns.
func TestStopAfterServingReturnsNoError(t *testing.T) {
	c := &Connector{}
	settings := map[string]any{"host": "127.0.0.1", "port": 0}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	out := make(chan *types.Message, 1)
	src, err := c.NewSource(types.SourceConfig{Type: "http", Settings: map[string]any{"path": "/ping"}}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	if err := src.Start(context.Background()); err != nil {
		t.Fatalf("source Start: %v", err)
	}

	// Drive one request through: Shutdown only reports the double close for a
	// listener it is tracking, and it takes ownership when Serve accepts it.
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://"+c.ln.Addr().String()+"/ping", bytes.NewReader([]byte(`{}`)))
	if resp := do(t, req); resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}

	// The shutdown order the runtime uses: sources drain first, then connectors.
	if err := src.Stop(context.Background()); err != nil {
		t.Fatalf("source Stop: %v", err)
	}
	if err := c.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after serving: %v", err)
	}
}

// TestStopReleasesPortAfterServing is the reload case of the same contract: a
// connector that did serve must also have released its port by the time Stop
// returns. Shutdown closes the listeners it is tracking, and the accept loop
// tracks c.ln — but only once it reaches Serve, which claiming serveOnce does not
// prove. Stop returning while that goroutine was still being scheduled left the
// port held for a moment longer, and the next generation's Start raced it for the
// bind. Stop now waits for the accept loop to return.
func TestStopReleasesPortAfterServing(t *testing.T) {
	port := freePort(t)
	settings := map[string]any{"host": "127.0.0.1", "port": port}

	first := &Connector{}
	if err := first.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	out := make(chan *types.Message, 1)
	src, err := first.NewSource(types.SourceConfig{Type: "http", Settings: map[string]any{"path": "/ping"}}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	if err := src.Start(context.Background()); err != nil {
		t.Fatalf("source Start: %v", err)
	}

	// Serve one request, so the listener is genuinely owned by the accept loop.
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://"+first.ln.Addr().String()+"/ping", bytes.NewReader([]byte(`{}`)))
	if resp := do(t, req); resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}

	if err := src.Stop(context.Background()); err != nil {
		t.Fatalf("source Stop: %v", err)
	}
	if err := first.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after serving: %v", err)
	}

	// No retry loop: the point is that the port is free the instant Stop returns.
	second := &Connector{}
	if err := second.Start(context.Background(), types.ConnectorConfig{Settings: settings}); err != nil {
		t.Fatalf("re-bind %d after a serving connector stopped: %v", port, err)
	}
	if err := second.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

// freePort binds an ephemeral loopback port, closes it, and returns the number
// so a test can re-bind it deterministically.
func freePort(t *testing.T) int {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return port
}

type response struct {
	status int
	body   []byte
	header http.Header
}

func do(t *testing.T, req *http.Request) response {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return response{status: resp.StatusCode, body: body, header: resp.Header}
}
