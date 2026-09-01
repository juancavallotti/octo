package parallel

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// startConnector starts a parallel connector with the given settings and
// registers cleanup.
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
	if _, err := core.DefaultRegistry().New("parallel"); err != nil {
		t.Fatalf("connector %q not registered: %v", "parallel", err)
	}
}

func TestConnectorRequiresAPIKey(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{}}); err == nil {
		t.Error("expected an error when apiKey is missing")
	}
}

func TestConnectorParsesTimeout(t *testing.T) {
	c := startConnector(t, map[string]any{"apiKey": "pk-test", "timeout": "90s"})
	if c.client.Timeout != 90*time.Second {
		t.Errorf("timeout = %v, want 90s", c.client.Timeout)
	}
}

func TestConnectorDecodesWebhookSecret(t *testing.T) {
	t.Run("whsec_ carries base64 key material", func(t *testing.T) {
		// Built rather than written out: the point is that whsec_ hides base64 key
		// material, and a literal would hide that relationship (and read to gosec
		// as a real credential).
		secret := secretPrefix + base64.StdEncoding.EncodeToString([]byte("secret-key"))
		c := startConnector(t, map[string]any{"apiKey": "pk-test", "webhookSecret": secret})
		if string(c.webhookKey) != "secret-key" {
			t.Errorf("webhookKey = %q, want the decoded bytes", c.webhookKey)
		}
	})

	t.Run("an unprefixed secret is used verbatim", func(t *testing.T) {
		c := startConnector(t, map[string]any{"apiKey": "pk-test", "webhookSecret": "plain-secret"})
		if string(c.webhookKey) != "plain-secret" {
			t.Errorf("webhookKey = %q, want the raw bytes", c.webhookKey)
		}
	})

	t.Run("a malformed whsec_ secret fails at startup", func(t *testing.T) {
		c := &Connector{}
		err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{
			"apiKey":        "pk-test",
			"webhookSecret": secretPrefix + "not!base64!",
		}})
		if err == nil {
			t.Fatal("expected a malformed secret to fail Start")
		}
		if !strings.Contains(err.Error(), "base64") {
			t.Errorf("error = %q, want it to say what is wrong", err)
		}
	})

	t.Run("no secret is allowed", func(t *testing.T) {
		c := startConnector(t, map[string]any{"apiKey": "pk-test"})
		if c.HasWebhookSecret() {
			t.Error("HasWebhookSecret should be false when none is configured")
		}
	})
}

func TestCallPostsJSONWithAPIKeyHeader(t *testing.T) {
	var gotKey, gotMethod, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("x-api-key")
		gotMethod = r.Method
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"search_id":"s1","results":[]}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"apiKey": "pk-test", "apiBaseURL": srv.URL})
	resp, err := c.Call(context.Background(), "v1/search", map[string]any{"objective": "octopuses"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotKey != "pk-test" {
		t.Errorf("x-api-key = %q, want pk-test", gotKey)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1/search" {
		t.Errorf("path = %q, want /v1/search", gotPath)
	}
	if gotBody["objective"] != "octopuses" {
		t.Errorf("body objective = %v, want octopuses", gotBody["objective"])
	}
	if resp["search_id"] != "s1" {
		t.Errorf("resp search_id = %v, want s1", resp["search_id"])
	}
}

func TestCallSurfacesAPIError(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"detail string", `{"detail":"Invalid API key"}`, "Invalid API key"},
		{"detail object", `{"detail":{"message":"quota exceeded"}}`, "quota exceeded"},
		{"bare error", `{"error":"nope"}`, "nope"},
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

			c := startConnector(t, map[string]any{"apiKey": "pk-test", "apiBaseURL": srv.URL})
			_, err := c.Call(context.Background(), "v1/search", map[string]any{})
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
	if _, err := c.Call(context.Background(), "v1/search", map[string]any{}); err == nil {
		t.Error("expected an error when the connector was never started")
	}
}
