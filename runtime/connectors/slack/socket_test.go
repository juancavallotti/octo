package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// stubSlack stands in for Slack's side of Socket Mode: apps.connections.open
// hands out the stub's own websocket URL, and every accepted websocket is passed
// to the test to drive.
type stubSlack struct {
	srv     *httptest.Server
	opens   atomic.Int32
	sockets chan *websocket.Conn
}

func newStubSlack(t *testing.T) *stubSlack {
	t.Helper()
	stub := &stubSlack{sockets: make(chan *websocket.Conn, 4)}
	upgrader := websocket.Upgrader{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xapp-test" {
			_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
			return
		}
		stub.opens.Add(1)
		url := "ws" + strings.TrimPrefix(stub.srv.URL, "http") + "/socket"
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "url": url})
	})
	mux.HandleFunc("GET /socket", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		stub.sockets <- ws
	})
	stub.srv = httptest.NewServer(mux)
	t.Cleanup(stub.srv.Close)
	return stub
}

// accept waits for the source's next websocket and greets it as Slack does.
func (s *stubSlack) accept(t *testing.T) *websocket.Conn {
	t.Helper()
	select {
	case ws := <-s.sockets:
		t.Cleanup(func() { _ = ws.Close() })
		if err := ws.WriteJSON(map[string]any{"type": "hello"}); err != nil {
			t.Fatalf("write hello: %v", err)
		}
		return ws
	case <-time.After(5 * time.Second):
		t.Fatal("source never connected")
		return nil
	}
}

// startSocketSource starts a slack source against stub and returns its output
// channel.
func startSocketSource(t *testing.T, stub *stubSlack, settings types.Settings) chan *types.Message {
	t.Helper()
	conn := startConnector(t, map[string]any{
		"botToken":   "xoxb-test",
		"appToken":   "xapp-test",
		"apiBaseURL": stub.srv.URL,
	})
	out := make(chan *types.Message, 4)
	src, err := conn.NewSource(types.SourceConfig{Type: socketSourceType, Settings: settings}, out, core.SourceDeps{})
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	if err := src.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = src.Stop(context.Background()) })
	return out
}

func sendEnvelope(t *testing.T, ws *websocket.Conn, envType, id, payload string, acceptsResponse bool) {
	t.Helper()
	err := ws.WriteJSON(map[string]any{
		"type":                     envType,
		"envelope_id":              id,
		"payload":                  json.RawMessage(payload),
		"accepts_response_payload": acceptsResponse,
	})
	if err != nil {
		t.Fatalf("write envelope: %v", err)
	}
}

func readAck(t *testing.T, ws *websocket.Conn) map[string]any {
	t.Helper()
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var got map[string]any
	if err := ws.ReadJSON(&got); err != nil {
		t.Fatalf("read ack: %v", err)
	}
	return got
}

func receive(t *testing.T, out chan *types.Message) *types.Message {
	t.Helper()
	select {
	case msg := <-out:
		return msg
	case <-time.After(5 * time.Second):
		t.Fatal("no message reached the flow")
		return nil
	}
}

func TestSocketEventIsAckedOnReceipt(t *testing.T) {
	stub := newStubSlack(t)
	out := startSocketSource(t, stub, nil)
	ws := stub.accept(t)

	sendEnvelope(t, ws, "events_api", "env-1",
		`{"type":"event_callback","event":{"type":"app_mention","text":"hi"}}`, false)

	// No flow result is ever published, so the ack can only be the on-receipt one.
	if got := readAck(t, ws); got["envelope_id"] != "env-1" || got["payload"] != nil {
		t.Errorf("ack = %v, want a bare ack for env-1", got)
	}
	msg := receive(t, out)
	body, _ := msg.Body.(map[string]any)
	if body["type"] != "event_callback" {
		t.Errorf("body = %v, want the event_callback payload", msg.Body)
	}
	if got, _ := msg.Variables.String(envelopeTypeVar); got != "events_api" {
		t.Errorf("%s = %q, want events_api", envelopeTypeVar, got)
	}
}

func TestSocketSlashCommandAckCarriesFlowResult(t *testing.T) {
	stub := newStubSlack(t)
	out := startSocketSource(t, stub, nil)
	ws := stub.accept(t)

	sendEnvelope(t, ws, "slash_commands", "env-2", `{"command":"/deploy","text":"staging"}`, true)

	msg := receive(t, out)
	if body, _ := msg.Body.(map[string]any); body["command"] != "/deploy" {
		t.Fatalf("body = %v, want the slash command fields", msg.Body)
	}
	result := msg.Clone()
	result.SetBody(map[string]any{"response_type": "ephemeral", "text": "on it"})
	core.DefaultEventBus().Publish(types.FlowEvent{
		Kind: types.FlowEventCompleted, EventID: msg.EventID, Result: result,
	})

	got := readAck(t, ws)
	want := map[string]any{"response_type": "ephemeral", "text": "on it"}
	payload, _ := got["payload"].(map[string]any)
	if got["envelope_id"] != "env-2" || payload["text"] != want["text"] || payload["response_type"] != want["response_type"] {
		t.Errorf("ack = %v, want env-2 carrying %v", got, want)
	}
}

func TestSocketSlowFlowIsAckedWithoutPayload(t *testing.T) {
	stub := newStubSlack(t)
	out := startSocketSource(t, stub, types.Settings{"ackTimeout": "50ms"})
	ws := stub.accept(t)

	sendEnvelope(t, ws, "slash_commands", "env-3", `{"command":"/deploy"}`, true)
	receive(t, out) // the flow takes it and never finishes

	if got := readAck(t, ws); got["envelope_id"] != "env-3" || got["payload"] != nil {
		t.Errorf("ack = %v, want a bare ack for env-3 once the timeout passes", got)
	}
}

func TestSocketReconnectsOnDisconnect(t *testing.T) {
	stub := newStubSlack(t)
	out := startSocketSource(t, stub, nil)

	first := stub.accept(t)
	if err := first.WriteJSON(map[string]any{"type": "disconnect", "reason": "refresh_requested"}); err != nil {
		t.Fatalf("write disconnect: %v", err)
	}

	// The source opens a fresh URL and keeps receiving on the new connection.
	second := stub.accept(t)
	if n := stub.opens.Load(); n != 2 {
		t.Errorf("apps.connections.open called %d times, want 2", n)
	}
	sendEnvelope(t, second, "events_api", "env-4", `{"type":"event_callback"}`, false)
	if got := readAck(t, second); got["envelope_id"] != "env-4" {
		t.Errorf("ack = %v, want env-4 on the new connection", got)
	}
	receive(t, out)
}

func TestSocketSourceRequiresAppToken(t *testing.T) {
	conn := startConnector(t, map[string]any{"botToken": "xoxb-test"})
	_, err := conn.NewSource(types.SourceConfig{Type: socketSourceType}, make(chan *types.Message), core.SourceDeps{})
	if err == nil {
		t.Error("expected an error when the connector has no app token")
	}
}
