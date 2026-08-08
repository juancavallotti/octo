package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// frame is one SSE frame: an event name and its JSON payload.
type frame struct{ event, data string }

// writeSSE serves Anthropic-format frames.
//
// Two things about the format are easy to get wrong and silent when you do. The
// event: line is mandatory here, unlike the other two providers: this SDK's stream
// dispatches on the SSE event name rather than on the JSON type field, so a frame
// without one matches no case and is dropped with no error at all. And a data
// field ends at its newline, so the payload is compacted — the fixtures below are
// written across lines to stay readable, which would otherwise truncate every
// frame at its first line break.
func writeSSE(t *testing.T, w http.ResponseWriter, frames []frame) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	for _, f := range frames {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(f.data)); err != nil {
			t.Errorf("fixture frame %q is not valid JSON: %v", f.event, err)
			return
		}
		_, _ = io.WriteString(w, "event: "+f.event+"\ndata: "+buf.String()+"\n\n")
	}
}

// The same turn in both shapes: a thinking block, an answer, and a tool call whose
// arguments arrive in fragments.
var (
	streamedTurn = []frame{
		{"message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message",
			"role":"assistant","model":"claude-sonnet-4-6","content":[],"stop_reason":null,
			"usage":{"input_tokens":5,"output_tokens":1}}}`},

		{"content_block_start", `{"type":"content_block_start","index":0,
			"content_block":{"type":"thinking","thinking":"","signature":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,
			"delta":{"type":"thinking_delta","thinking":"the user wants "}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,
			"delta":{"type":"thinking_delta","thinking":"billing"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,
			"delta":{"type":"signature_delta","signature":"sig-abc"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},

		{"content_block_start", `{"type":"content_block_start","index":1,
			"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":1,
			"delta":{"type":"text_delta","text":"rou"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":1,
			"delta":{"type":"text_delta","text":"ting"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":1}`},

		{"content_block_start", `{"type":"content_block_start","index":2,
			"content_block":{"type":"tool_use","id":"tu_1","name":"select_route","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":2,
			"delta":{"type":"input_json_delta","partial_json":"{\"route\":"}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":2,
			"delta":{"type":"input_json_delta","partial_json":"\"billing\"}"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":2}`},

		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use"},
			"usage":{"output_tokens":30,"output_tokens_details":{"thinking_tokens":20}}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}

	blockingTurn = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6",
		"content":[
			{"type":"thinking","thinking":"the user wants billing","signature":"sig-abc"},
			{"type":"text","text":"routing"},
			{"type":"tool_use","id":"tu_1","name":"select_route","input":{"route":"billing"}}],
		"stop_reason":"tool_use",
		"usage":{"input_tokens":5,"output_tokens":30,"output_tokens_details":{"thinking_tokens":20}}}`
)

// turnServer answers either shape from one endpoint, choosing by the stream flag
// the SDK sets, so both paths are driven against the same fixture.
func turnServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if stream, _ := req["stream"].(bool); stream {
			writeSSE(t, w, streamedTurn)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, blockingTurn)
	}))
}

func streamConnector(t *testing.T, url string) *Connector {
	t.Helper()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "claude",
		Settings: types.Settings{"apiKey": "sk-test", "baseURL": url, "thinking": thinkingAdaptive},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return c
}

// TestStreamMatchesComplete is the property the whole feature rests on: for the
// same turn, streaming returns exactly what the blocking call would have. Events
// are additive observation, so switching a caller to Stream can change when it
// learns something but never what it learns.
func TestStreamMatchesComplete(t *testing.T) {
	srv := turnServer(t)
	defer srv.Close()
	c := streamConnector(t, srv.URL)

	req := core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "billing question"}}}

	blocking, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	streamed, err := c.Stream(context.Background(), req, func(core.LLMStreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}

	if !reflect.DeepEqual(blocking, streamed) {
		t.Errorf("streamed response differs from blocking\n blocking = %+v\n streamed = %+v", blocking, streamed)
	}
	// Guard against the comparison passing because both are empty.
	if streamed.Text != "routing" || len(streamed.ToolCalls) != 1 || streamed.Usage == nil {
		t.Fatalf("fixture did not translate: %+v", streamed)
	}
}

// TestStreamEmitsCanonicalEvents pins the mapping from Anthropic's wire events to
// the canonical vocabulary, including that a tool_input fragment knows which call
// it belongs to and that bookkeeping events produce nothing.
func TestStreamEmitsCanonicalEvents(t *testing.T) {
	srv := turnServer(t)
	defer srv.Close()
	c := streamConnector(t, srv.URL)

	var got []core.LLMStreamEvent
	if _, err := c.Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(ev core.LLMStreamEvent) error {
			got = append(got, ev)
			return nil
		}); err != nil {
		t.Fatalf("stream: %v", err)
	}

	want := []core.LLMStreamEvent{
		{Kind: core.LLMStreamThinking, Text: "the user wants ", Index: 0},
		{Kind: core.LLMStreamThinking, Text: "billing", Index: 0},
		{Kind: core.LLMStreamText, Text: "rou", Index: 1},
		{Kind: core.LLMStreamText, Text: "ting", Index: 1},
		{Kind: core.LLMStreamToolInput, Text: `{"route":`, Tool: "select_route", ToolCallID: "tu_1", Index: 2},
		{Kind: core.LLMStreamToolInput, Text: `"billing"}`, Tool: "select_route", ToolCallID: "tu_1", Index: 2},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events =\n%+v\nwant\n%+v", got, want)
	}
}

// TestStreamStopsWhenHandlerErrors pins the cancellation path: a caller with
// nobody left to report to stops the turn rather than reading it to the end.
func TestStreamStopsWhenHandlerErrors(t *testing.T) {
	srv := turnServer(t)
	defer srv.Close()
	c := streamConnector(t, srv.URL)

	sentinel := io.ErrClosedPipe
	seen := 0
	_, err := c.Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error {
			seen++
			return sentinel
		})
	if err == nil {
		t.Fatal("expected the handler's error to end the stream")
	}
	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Errorf("error = %v, want the handler's error returned as-is", err)
	}
	if seen != 1 {
		t.Errorf("handler called %d times, want 1 — the stream should stop on the first error", seen)
	}
}
