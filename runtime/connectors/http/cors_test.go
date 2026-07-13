package http

import (
	"context"
	"net/http"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// corsSettings used across the tests: two explicit origins, credentials on.
func corsTestSettings() map[string]any {
	return map[string]any{
		"cors": map[string]any{
			"allowedOrigins":   []string{"https://app.example"},
			"allowedMethods":   []string{"GET", "POST"},
			"allowedHeaders":   []string{"Content-Type", "X-Custom"},
			"exposedHeaders":   []string{"X-Total-Count"},
			"allowCredentials": true,
			"maxAge":           "600s",
		},
	}
}

func TestCORSPreflight(t *testing.T) {
	c, base := startConnector(t, corsTestSettings())
	// A route must exist to be realistic, but preflight is answered by the
	// middleware and never reaches the source, so no worker is needed.
	newSource(t, c, map[string]any{"path": "/orders"})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodOptions, base+"/orders", nil)
	req.Header.Set("Origin", "https://app.example")
	req.Header.Set("Access-Control-Request-Method", "POST")

	resp := do(t, req)
	if resp.status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", resp.status, resp.body)
	}
	if got := resp.header.Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin = %q, want https://app.example", got)
	}
	if got := resp.header.Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Errorf("Allow-Methods = %q, want \"GET, POST\"", got)
	}
	if got := resp.header.Get("Access-Control-Allow-Headers"); got != "Content-Type, X-Custom" {
		t.Errorf("Allow-Headers = %q, want \"Content-Type, X-Custom\"", got)
	}
	if got := resp.header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Allow-Credentials = %q, want true", got)
	}
	if got := resp.header.Get("Access-Control-Max-Age"); got != "600" {
		t.Errorf("Max-Age = %q, want 600", got)
	}
}

// TestCORSPreflightEchoesRequestHeaders verifies that when allowedHeaders is
// unset, the requested headers are echoed back on preflight.
func TestCORSPreflightEchoesRequestHeaders(t *testing.T) {
	c, base := startConnector(t, map[string]any{
		"cors": map[string]any{"allowedOrigins": []string{"*"}},
	})
	newSource(t, c, map[string]any{"path": "/orders"})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodOptions, base+"/orders", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	req.Header.Set("Access-Control-Request-Method", "DELETE")
	req.Header.Set("Access-Control-Request-Headers", "X-Requested-With, Authorization")

	resp := do(t, req)
	if resp.status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", resp.status, resp.body)
	}
	// Any-origin without credentials collapses to "*".
	if got := resp.header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Allow-Origin = %q, want *", got)
	}
	if got := resp.header.Get("Access-Control-Allow-Headers"); got != "X-Requested-With, Authorization" {
		t.Errorf("Allow-Headers = %q, want echoed request headers", got)
	}
}

// TestCORSOptionsNeverReachesFlow verifies that with CORS enabled, an OPTIONS
// request without the preflight header is still answered at the middleware (204)
// and no message is ever sent to the flow.
func TestCORSOptionsNeverReachesFlow(t *testing.T) {
	c, base := startConnector(t, corsTestSettings())
	out := newSource(t, c, map[string]any{"path": "/orders"})
	go func() {
		if msg, ok := <-out; ok {
			t.Errorf("OPTIONS unexpectedly reached the flow: %v", msg)
		}
	}()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodOptions, base+"/orders", nil)
	req.Header.Set("Origin", "https://app.example")
	// Note: no Access-Control-Request-Method header — a bare OPTIONS.

	resp := do(t, req)
	if resp.status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", resp.status, resp.body)
	}
	if got := resp.header.Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin = %q, want https://app.example", got)
	}
}

func TestCORSActualRequestAllowed(t *testing.T) {
	c, base := startConnector(t, corsTestSettings())
	out := newSource(t, c, map[string]any{"path": "/orders"})
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = map[string]any{"ok": true}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/orders", nil)
	req.Header.Set("Origin", "https://app.example")

	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	if got := resp.header.Get("Access-Control-Allow-Origin"); got != "https://app.example" {
		t.Errorf("Allow-Origin = %q, want https://app.example", got)
	}
	if got := resp.header.Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
	if got := resp.header.Get("Access-Control-Expose-Headers"); got != "X-Total-Count" {
		t.Errorf("Expose-Headers = %q, want X-Total-Count", got)
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	c, base := startConnector(t, corsTestSettings())
	out := newSource(t, c, map[string]any{"path": "/orders"})
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = map[string]any{"ok": true}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/orders", nil)
	req.Header.Set("Origin", "https://evil.example")

	resp := do(t, req)
	// The flow still runs; only the CORS headers are withheld so the browser blocks it.
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	if got := resp.header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for disallowed origin", got)
	}
}

// TestCORSDisabled verifies that with no CORS config the middleware is a
// pass-through: an actual request carries no CORS headers.
func TestCORSDisabled(t *testing.T) {
	c, base := startConnector(t, nil)
	out := newSource(t, c, map[string]any{"path": "/orders"})
	echoWorker(out, func(msg *types.Message) types.FlowEvent {
		msg.Body = map[string]any{"ok": true}
		return types.FlowEvent{Kind: types.FlowEventCompleted, Result: msg}
	})

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, base+"/orders", nil)
	req.Header.Set("Origin", "https://app.example")
	resp := do(t, req)
	if resp.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", resp.status, resp.body)
	}
	if got := resp.header.Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty when CORS disabled", got)
	}
}
