package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// blockDeps starts a slack connector pointed at baseURL and returns BlockDeps
// that resolve it under the name "slack".
func blockDeps(t *testing.T, baseURL string) core.BlockDeps {
	t.Helper()
	conn := startConnector(t, map[string]any{
		"botToken":      "xoxb-test",
		"signingSecret": "secret",
		"apiBaseURL":    baseURL,
	})
	return core.BlockDeps{Connector: func(name string) (core.Connector, bool) {
		if name == "slack" {
			return conn, true
		}
		return nil, false
	}}
}

func blockMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

func TestSendMessagePostsAndFoldsResult(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"channel":"C123","ts":"1622.45"}`))
	}))
	defer srv.Close()

	proc, err := newSendMessage(types.Settings{
		"connector": "slack",
		"target":    "body.channel",
		"text":      `"hello " + body.name`,
		"threadTs":  "vars.thread",
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSendMessage: %v", err)
	}

	msg := blockMessage(t, map[string]any{"channel": "C123", "name": "ada"})
	msg.Variables.Set("thread", "1600.00")

	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	if gotPath != "/chat.postMessage" {
		t.Errorf("path = %q, want /chat.postMessage", gotPath)
	}
	if gotBody["channel"] != "C123" {
		t.Errorf("channel = %v, want C123", gotBody["channel"])
	}
	if gotBody["text"] != "hello ada" {
		t.Errorf("text = %v, want 'hello ada'", gotBody["text"])
	}
	if gotBody["thread_ts"] != "1600.00" {
		t.Errorf("thread_ts = %v, want 1600.00", gotBody["thread_ts"])
	}
	if out.Variables[sendChannelVar] != "C123" {
		t.Errorf("%s = %v, want C123", sendChannelVar, out.Variables[sendChannelVar])
	}
	if out.Variables[sendTSVar] != "1622.45" {
		t.Errorf("%s = %v, want 1622.45", sendTSVar, out.Variables[sendTSVar])
	}
}

func TestSendMessageRequiresTargetAndText(t *testing.T) {
	deps := blockDeps(t, "http://unused")
	if _, err := newSendMessage(types.Settings{"connector": "slack", "text": `"hi"`}, deps); err == nil {
		t.Error("expected an error when target is missing")
	}
	if _, err := newSendMessage(types.Settings{"connector": "slack", "target": "body.c"}, deps); err == nil {
		t.Error("expected an error when both text and blocks are missing")
	}
}

func TestSendMessageFailOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"error":"channel_not_found"}`))
	}))
	defer srv.Close()

	// Default: a Slack error aborts the message.
	proc, err := newSendMessage(types.Settings{
		"connector": "slack", "target": `"C1"`, "text": `"hi"`,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSendMessage: %v", err)
	}
	if _, err := proc.Process(context.Background(), blockMessage(t, nil)); err == nil {
		t.Error("expected Process to fail on a Slack error by default")
	}

	// failOnError=false: pass the message through despite the Slack error.
	lenient, err := newSendMessage(types.Settings{
		"connector": "slack", "target": `"C1"`, "text": `"hi"`, "failOnError": false,
	}, blockDeps(t, srv.URL))
	if err != nil {
		t.Fatalf("newSendMessage: %v", err)
	}
	out, err := lenient.Process(context.Background(), blockMessage(t, nil))
	if err != nil {
		t.Errorf("Process with failOnError=false returned error: %v", err)
	}
	if out == nil {
		t.Error("expected the message to pass through with failOnError=false")
	}
}
