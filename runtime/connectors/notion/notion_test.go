package notion

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// startConnector starts a notion connector with the given settings and registers
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
	if _, err := core.DefaultRegistry().New("notion"); err != nil {
		t.Fatalf("connector %q not registered: %v", "notion", err)
	}
}

func TestConnectorRequiresToken(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{}}); err == nil {
		t.Error("expected an error when token is missing")
	}
}

func TestCallGet(t *testing.T) {
	var gotAuth, gotVersion, gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("Notion-Version")
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"page","id":"p1"}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"token": "ntn-test", "apiBaseURL": srv.URL})
	resp, err := c.Call(context.Background(), http.MethodGet, "pages/p1", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotAuth != "Bearer ntn-test" {
		t.Errorf("Authorization = %q, want Bearer ntn-test", gotAuth)
	}
	if gotVersion != defaultNotionVersion {
		t.Errorf("Notion-Version = %q, want %q", gotVersion, defaultNotionVersion)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/pages/p1" {
		t.Errorf("path = %q, want /pages/p1", gotPath)
	}
	if resp["id"] != "p1" {
		t.Errorf("resp id = %v, want p1", resp["id"])
	}
}

func TestCallPostSendsBody(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","results":[]}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"token": "ntn-test", "apiBaseURL": srv.URL})
	_, err := c.Call(context.Background(), http.MethodPost, "data_sources/d1/query",
		map[string]any{"page_size": 5})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotBody["page_size"] != float64(5) {
		t.Errorf("page_size = %v, want 5", gotBody["page_size"])
	}
}

func TestCallError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"object":"error","status":404,"code":"object_not_found","message":"Not found"}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"token": "ntn-test", "apiBaseURL": srv.URL})
	_, err := c.Call(context.Background(), http.MethodGet, "pages/missing", nil)
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
	if !strings.Contains(err.Error(), "object_not_found") {
		t.Errorf("error = %v, want it to mention object_not_found", err)
	}
}

func TestNotionVersionOverride(t *testing.T) {
	var gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("Notion-Version")
		_, _ = w.Write([]byte(`{"object":"page"}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{
		"token": "ntn-test", "apiBaseURL": srv.URL, "notionVersion": "2022-06-28",
	})
	if _, err := c.Call(context.Background(), http.MethodGet, "pages/p1", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotVersion != "2022-06-28" {
		t.Errorf("Notion-Version = %q, want 2022-06-28", gotVersion)
	}
}

func TestNotionVersionStripsTimestamp(t *testing.T) {
	// An unquoted date in YAML is decoded as a timestamp and reaches settings as an
	// RFC3339 string; the connector must send Notion just the date.
	var gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("Notion-Version")
		_, _ = w.Write([]byte(`{"object":"page"}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{
		"token": "ntn-test", "apiBaseURL": srv.URL, "notionVersion": "2025-09-03T00:00:00Z",
	})
	if _, err := c.Call(context.Background(), http.MethodGet, "pages/p1", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotVersion != "2025-09-03" {
		t.Errorf("Notion-Version = %q, want 2025-09-03", gotVersion)
	}
}

func TestVerifySignature(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	c := startConnector(t, map[string]any{"token": "ntn-test", "verificationToken": secret})

	body := []byte(`{"type":"page.updated","entity":{"id":"p1","type":"page"}}`)
	sig := computeSig(secret, body)

	if !c.VerifySignature(sig, body) {
		t.Error("valid signature was rejected")
	}
	if c.VerifySignature("sha256=deadbeef", body) {
		t.Error("bad signature was accepted")
	}
	if c.VerifySignature(sig, []byte("tampered")) {
		t.Error("tampered body was accepted")
	}
}

func TestVerifySignatureNoToken(t *testing.T) {
	c := startConnector(t, map[string]any{"token": "ntn-test"})
	if c.VerifySignature("sha256=whatever", []byte("x")) {
		t.Error("signature verified without a configured verification token")
	}
}

// computeSig builds the Notion signature the connector should accept.
func computeSig(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
