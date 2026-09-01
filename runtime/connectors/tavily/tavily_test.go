package tavily

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// startConnector starts a tavily connector with the given settings and registers
// cleanup.
func startConnector(t *testing.T, set map[string]any) *Connector {
	t.Helper()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: set}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = c.Stop(context.Background()) })
	return c
}

func TestConnectorRegistered(t *testing.T) {
	if _, err := core.DefaultRegistry().New("tavily"); err != nil {
		t.Fatalf("connector %q not registered: %v", "tavily", err)
	}
}

func TestConnectorRequiresAPIKey(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{}}); err == nil {
		t.Error("expected an error when apiKey is missing")
	}
}

func TestConnectorParsesTimeout(t *testing.T) {
	c := startConnector(t, map[string]any{"apiKey": "tvly-test", "timeout": "150s"})
	if c.client.Timeout != 150*time.Second {
		t.Errorf("timeout = %v, want 150s", c.client.Timeout)
	}
}

func TestCallPostsJSON(t *testing.T) {
	var gotAuth, gotMethod, gotPath, gotContentType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"query":"octo","results":[{"url":"https://example.com"}]}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"apiKey": "tvly-test", "apiBaseURL": srv.URL})
	resp, err := c.Call(context.Background(), "search", map[string]any{"query": "octo"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotAuth != "Bearer tvly-test" {
		t.Errorf("Authorization = %q, want Bearer tvly-test", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/search" {
		t.Errorf("path = %q, want /search", gotPath)
	}
	if gotBody["query"] != "octo" {
		t.Errorf("body query = %v, want octo", gotBody["query"])
	}
	if resp["query"] != "octo" {
		t.Errorf("resp query = %v, want octo", resp["query"])
	}
}

func TestCallTrimsTrailingSlashFromBaseURL(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"apiKey": "tvly-test", "apiBaseURL": srv.URL + "/"})
	if _, err := c.Call(context.Background(), "map", map[string]any{}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotPath != "/map" {
		t.Errorf("path = %q, want /map", gotPath)
	}
}

func TestCallSurfacesAPIError(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"nested detail", `{"detail":{"error":"Invalid API key"}}`, "Invalid API key"},
		{"plain detail", `{"detail":"Unprocessable"}`, "Unprocessable"},
		{"bare error", `{"error":"rate limited"}`, "rate limited"},
		{"no message", `{}`, "401"},
		// A gateway in front of the API answers with HTML, not JSON. The status is
		// the useful half of that failure and must survive.
		{"a non-JSON gateway page", `<html><body>502 Bad Gateway</body></html>`, "401"},
		{"an empty body", ``, "401"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			c := startConnector(t, map[string]any{"apiKey": "tvly-test", "apiBaseURL": srv.URL})
			_, err := c.Call(context.Background(), "search", map[string]any{})
			if err == nil {
				t.Fatal("expected an error for a 401 response")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestCallBeforeStart(t *testing.T) {
	c := &Connector{}
	if _, err := c.Call(context.Background(), "search", map[string]any{}); err == nil {
		t.Error("expected an error when the connector was never started")
	}
}
