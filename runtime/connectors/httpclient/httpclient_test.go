package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/eip-go/types"
)

// startConnector starts an http-client connector with the given settings and
// registers cleanup. baseURL is filled in by the caller via settings.
func startConnector(t *testing.T, settings types.Settings) *Connector {
	t.Helper()
	c := &Connector{}
	cfg := types.ConnectorConfig{Name: "api", Type: "http-client", Settings: settings}
	if err := c.Start(context.Background(), cfg); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

func get(t *testing.T, c *Connector, path string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return resp.StatusCode, string(body)
}

func TestResolvesBasePathAndBearerAuth(t *testing.T) {
	var gotPath, gotAuth, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotHeader = r.Header.Get("X-App")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{
		"baseURL": srv.URL + "/v1",
		"auth":    map[string]any{"type": "bearer", "token": "secret-token"},
		"headers": map[string]any{"X-App": "eip"},
	})

	status, body := get(t, c, "/things")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if gotPath != "/v1/things" {
		t.Errorf("path = %q, want /v1/things (base path must be preserved)", gotPath)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("auth = %q, want Bearer secret-token", gotAuth)
	}
	if gotHeader != "eip" {
		t.Errorf("default header X-App = %q, want eip", gotHeader)
	}
	if body != `{"ok":true}` {
		t.Errorf("body = %q", body)
	}
}

func TestBasicAuth(t *testing.T) {
	var okUser, okPass bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		okUser = ok && u == "alice"
		okPass = ok && p == "wonderland"
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{
		"baseURL": srv.URL,
		"auth":    map[string]any{"type": "basic", "username": "alice", "password": "wonderland"},
	})
	get(t, c, "/")
	if !okUser || !okPass {
		t.Errorf("basic auth not received correctly (user=%v pass=%v)", okUser, okPass)
	}
}

func TestCacheServesSecondGetWithoutRoundTrip(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, `{"n":1}`)
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{
		"baseURL": srv.URL,
		"cache":   map[string]any{"enabled": true, "ttl": "1m"},
	})

	_, first := get(t, c, "/data")
	_, second := get(t, c, "/data")
	if hits != 1 {
		t.Errorf("server hits = %d, want 1 (second GET should be cached)", hits)
	}
	if first != second {
		t.Errorf("cached body mismatch: %q vs %q", first, second)
	}

	// A different URL is a separate cache key and must hit the server.
	get(t, c, "/other")
	if hits != 2 {
		t.Errorf("server hits = %d after distinct URL, want 2", hits)
	}
}

func TestMaxResponseBytesBoundsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 1000))
	}))
	defer srv.Close()

	c := startConnector(t, types.Settings{
		"baseURL":          srv.URL,
		"maxResponseBytes": 10,
	})
	_, body := get(t, c, "/big")
	if len(body) != 10 {
		t.Errorf("body length = %d, want 10 (bounded by maxResponseBytes)", len(body))
	}
}

func TestStartValidatesConfig(t *testing.T) {
	tests := []struct {
		name     string
		settings types.Settings
	}{
		{name: "missing baseURL", settings: types.Settings{}},
		{name: "relative baseURL", settings: types.Settings{"baseURL": "/no-host"}},
		{name: "bearer without token", settings: types.Settings{"baseURL": "https://x", "auth": map[string]any{"type": "bearer"}}},
		{name: "basic without username", settings: types.Settings{"baseURL": "https://x", "auth": map[string]any{"type": "basic", "password": "p"}}},
		{name: "unknown auth type", settings: types.Settings{"baseURL": "https://x", "auth": map[string]any{"type": "digest"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Connector{}
			if err := c.Start(context.Background(), types.ConnectorConfig{Name: "api", Settings: tt.settings}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
