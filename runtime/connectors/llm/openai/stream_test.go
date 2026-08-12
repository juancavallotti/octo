package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// writeSSE serves Responses-format events.
//
// The SDK reads the event name as well as the data here, so each frame carries
// both. The payload is compacted because a data field ends at its newline and the
// fixtures below are written across lines to stay readable.
func writeSSE(t *testing.T, w http.ResponseWriter, events []string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	for _, e := range events {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(e)); err != nil {
			t.Errorf("fixture event is not valid JSON: %v", err)
			return
		}
		var typed struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(buf.Bytes(), &typed)
		_, _ = io.WriteString(w, "event: "+typed.Type+"\ndata: "+buf.String()+"\n\n")
	}
}

// The finished turn, as the terminal response.completed event carries it and as
// the blocking call returns it — one object, used for both so the equality test
// below compares translation rather than two hand-written fixtures.
const finishedTurn = `{
	"id":"resp_1","object":"response","created_at":1,"model":"gpt-5.6-luna","status":"completed",
	"output":[
		{"type":"reasoning","id":"rs_1","encrypted_content":"gAAAA",
		 "summary":[{"type":"summary_text","text":"billing then"}]},
		{"type":"message","id":"msg_1","role":"assistant","status":"completed",
		 "content":[{"type":"output_text","text":"routing","annotations":[]}]},
		{"type":"function_call","id":"fc_1","call_id":"tc_1","name":"select_route",
		 "arguments":"{\"route\":\"billing\"}"}
	],
	"usage":{"input_tokens":5,"output_tokens":30,"total_tokens":35,
		"input_tokens_details":{"cached_tokens":3},
		"output_tokens_details":{"reasoning_tokens":20}}
}`

// The same turn streamed: a reasoning summary, an answer, then a tool call whose
// name and id are announced once by output_item.added and whose arguments dribble
// in after it.
var streamedEvents = []string{
	`{"type":"response.created","sequence_number":0,"response":{"id":"resp_1","object":"response",
		"created_at":1,"model":"gpt-5.6-luna","status":"in_progress","output":[]}}`,
	`{"type":"response.output_item.added","sequence_number":1,"output_index":0,
		"item":{"type":"reasoning","id":"rs_1","summary":[]}}`,
	`{"type":"response.reasoning_summary_text.delta","sequence_number":2,"item_id":"rs_1",
		"output_index":0,"summary_index":0,"delta":"billing "}`,
	`{"type":"response.reasoning_summary_text.delta","sequence_number":3,"item_id":"rs_1",
		"output_index":0,"summary_index":0,"delta":"then"}`,
	`{"type":"response.output_item.added","sequence_number":4,"output_index":1,
		"item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
	`{"type":"response.output_text.delta","sequence_number":5,"item_id":"msg_1",
		"output_index":1,"content_index":0,"delta":"rou"}`,
	`{"type":"response.output_text.delta","sequence_number":6,"item_id":"msg_1",
		"output_index":1,"content_index":0,"delta":"ting"}`,
	`{"type":"response.output_item.added","sequence_number":7,"output_index":2,
		"item":{"type":"function_call","id":"fc_1","call_id":"tc_1","name":"select_route","arguments":""}}`,
	`{"type":"response.function_call_arguments.delta","sequence_number":8,"item_id":"fc_1",
		"output_index":2,"delta":"{\"route\":"}`,
	`{"type":"response.function_call_arguments.delta","sequence_number":9,"item_id":"fc_1",
		"output_index":2,"delta":"\"billing\"}"}`,
	`{"type":"response.completed","sequence_number":10,"response":` + finishedTurn + `}`,
}

func turnServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if stream, _ := req["stream"].(bool); stream {
			writeSSE(t, w, streamedEvents)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, finishedTurn)
	}))
}

func streamConnector(t *testing.T, url string) *Connector {
	t.Helper()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "gpt",
		Settings: types.Settings{"apiKey": "sk-test", "baseURL": url},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return c
}

// TestStreamMatchesComplete is the property the whole feature rests on: for the
// same turn, streaming returns exactly what the blocking call would have.
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
	if streamed.Text != "routing" || streamed.StopReason != core.LLMStopToolUse || streamed.Usage == nil {
		t.Fatalf("fixture did not translate: %+v", streamed)
	}
	// The reasoning has to survive the streamed path too, or a tool loop on a
	// reasoning model breaks on its second turn with nothing to echo back.
	if len(streamed.Raw.Thinking) != 1 || streamed.Raw.Thinking[0].Signature != "rs_1" {
		t.Errorf("streamed turn lost its reasoning: %+v", streamed.Raw.Thinking)
	}
}

// TestStreamEmitsCanonicalEvents pins the mapping, in particular that every
// tool_input fragment names its call even though the name and id are announced
// only once, on the item that opens it.
//
// Thinking is here at all because Responses returns reasoning summaries; on Chat
// Completions there was no reasoning content of any kind, so this kind could never
// fire and an agent showed nothing at all while a reasoning model worked.
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
		{Kind: core.LLMStreamThinking, Text: "billing ", Index: 0},
		{Kind: core.LLMStreamThinking, Text: "then", Index: 0},
		{Kind: core.LLMStreamText, Text: "rou", Index: 1},
		{Kind: core.LLMStreamText, Text: "ting", Index: 1},
		{
			Kind: core.LLMStreamToolInput, Text: `{"route":`,
			Tool: "select_route", ToolCallID: "tc_1", Index: 2,
		},
		{
			Kind: core.LLMStreamToolInput, Text: `"billing"}`,
			Tool: "select_route", ToolCallID: "tc_1", Index: 2,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events =\n%+v\nwant\n%+v", got, want)
	}
}

// TestStreamWithoutACompletedEventFails pins that a stream which ends before the
// terminal event is an error rather than a half-translated turn. The finished
// response arrives whole on that event and nowhere else, so without it there is
// nothing to return — and returning an empty turn would look to a caller like a
// model that answered nothing.
func TestStreamWithoutACompletedEventFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(t, w, []string{
			`{"type":"response.output_item.added","sequence_number":0,"output_index":0,
				"item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
			`{"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1",
				"output_index":0,"content_index":0,"delta":"hi"}`,
		})
	}))
	defer srv.Close()

	_, err := streamConnector(t, srv.URL).Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error { return nil })
	if err == nil {
		t.Fatal("expected an error when the stream ends without a completed response")
	}
}

// TestStreamReportsAFailedResponse pins that a response the server gives up on
// reaches the caller as an error carrying the server's own message, rather than as
// the "ended without a completed response" the absent terminal event would
// otherwise produce.
func TestStreamReportsAFailedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(t, w, []string{
			`{"type":"response.failed","sequence_number":0,"response":{"id":"resp_1","object":"response",
				"created_at":1,"model":"m","status":"failed","output":[],
				"error":{"code":"server_error","message":"the model went away"}}}`,
		})
	}))
	defer srv.Close()

	_, err := streamConnector(t, srv.URL).Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error { return nil })
	if err == nil {
		t.Fatal("expected an error for a failed response")
	}
	if !bytes.Contains([]byte(err.Error()), []byte("the model went away")) {
		t.Errorf("error = %v, want it to carry the server's own message", err)
	}
}
