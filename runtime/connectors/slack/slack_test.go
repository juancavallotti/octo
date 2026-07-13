package slack

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
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// startConnector starts a slack connector with the given settings and registers
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
	if _, err := core.DefaultRegistry().New("slack"); err != nil {
		t.Fatalf("connector %q not registered: %v", "slack", err)
	}
}

func TestConnectorRequiresBotToken(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{Settings: map[string]any{}}); err == nil {
		t.Error("expected an error when botToken is missing")
	}
}

func TestCallSuccess(t *testing.T) {
	var gotAuth, gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C1","ts":"1622.33"}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"botToken": "xoxb-test", "apiBaseURL": srv.URL})
	resp, err := c.Call(context.Background(), "chat.postMessage", map[string]any{"channel": "C1", "text": "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotAuth != "Bearer xoxb-test" {
		t.Errorf("Authorization = %q, want Bearer xoxb-test", gotAuth)
	}
	if gotPath != "/chat.postMessage" {
		t.Errorf("path = %q, want /chat.postMessage", gotPath)
	}
	if gotBody["channel"] != "C1" || gotBody["text"] != "hi" {
		t.Errorf("payload = %v", gotBody)
	}
	if resp["ts"] != "1622.33" {
		t.Errorf("resp ts = %v, want 1622.33", resp["ts"])
	}
}

func TestCallError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()

	c := startConnector(t, map[string]any{"botToken": "xoxb-test", "apiBaseURL": srv.URL})
	_, err := c.Call(context.Background(), "chat.postMessage", map[string]any{})
	if err == nil {
		t.Fatal("expected an error for an ok:false response")
	}
	if !strings.Contains(err.Error(), "channel_not_found") {
		t.Errorf("error = %v, want it to mention channel_not_found", err)
	}
}

func TestVerifySignature(t *testing.T) {
	const secret = "8f742231b10e8888abcd99yyyzzz85a5"
	c := startConnector(t, map[string]any{"botToken": "xoxb-test", "signingSecret": secret})

	now := time.Unix(1531420618, 0)
	ts := "1531420618"
	body := []byte(`token=xyzz0WbapA4vBCDEFasx0q6G&team_id=T1DC2JH3J`)
	sig := computeSig(secret, ts, body)

	if !c.VerifySignature(sig, ts, body, now) {
		t.Error("valid signature was rejected")
	}
	if c.VerifySignature("v0=deadbeef", ts, body, now) {
		t.Error("bad signature was accepted")
	}
	if c.VerifySignature(sig, ts, body, now.Add(10*time.Minute)) {
		t.Error("expired timestamp was accepted")
	}
	if c.VerifySignature(sig, ts, []byte("tampered"), now) {
		t.Error("tampered body was accepted")
	}
}

func TestVerifySignatureNoSecret(t *testing.T) {
	c := startConnector(t, map[string]any{"botToken": "xoxb-test"})
	if c.VerifySignature("v0=whatever", "1531420618", []byte("x"), time.Unix(1531420618, 0)) {
		t.Error("signature verified without a configured signing secret")
	}
}

// computeSig builds the Slack v0 signature the connector should accept.
func computeSig(secret, ts string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("v0:" + ts + ":" + string(body)))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}
