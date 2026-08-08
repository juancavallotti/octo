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

// writeSSE serves OpenAI-format chunks.
//
// This SDK ignores the SSE event name entirely, so frames are bare data lines,
// terminated by the [DONE] sentinel. The payload is compacted because a data field
// ends at its newline and the fixtures below are written across lines to stay
// readable.
func writeSSE(t *testing.T, w http.ResponseWriter, chunks []string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	for _, c := range chunks {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(c)); err != nil {
			t.Errorf("fixture chunk is not valid JSON: %v", err)
			return
		}
		_, _ = io.WriteString(w, "data: "+buf.String()+"\n\n")
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// The same turn in both shapes: an answer, then a tool call whose id and name
// arrive on the first fragment and whose arguments dribble in after it.
var (
	streamedChunks = []string{
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4",
			"choices":[{"index":0,"delta":{"role":"assistant","content":"rou"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4",
			"choices":[{"index":0,"delta":{"content":"ting"}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4",
			"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"tc_1","type":"function",
				"function":{"name":"select_route","arguments":""}}]}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4",
			"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,
				"function":{"arguments":"{\"route\":"}}]}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4",
			"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,
				"function":{"arguments":"\"billing\"}"}}]}}]}`,
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4",
			"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
		// The usage chunk: choices empty, usage for the whole request.
		`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"gpt-5.4","choices":[],
			"usage":{"prompt_tokens":5,"completion_tokens":30,
				"completion_tokens_details":{"reasoning_tokens":20},
				"prompt_tokens_details":{"cached_tokens":3}}}`,
	}

	blockingTurn = `{"id":"c1","object":"chat.completion","created":1,"model":"gpt-5.4",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"routing",
			"tool_calls":[{"id":"tc_1","type":"function",
				"function":{"name":"select_route","arguments":"{\"route\":\"billing\"}"}}]}}],
		"usage":{"prompt_tokens":5,"completion_tokens":30,
			"completion_tokens_details":{"reasoning_tokens":20},
			"prompt_tokens_details":{"cached_tokens":3}}}`
)

func turnServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if stream, _ := req["stream"].(bool); stream {
			writeSSE(t, w, streamedChunks)
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
}

// TestStreamEmitsCanonicalEvents pins the mapping, in particular that every
// tool_input fragment names its call even though only the first one carries the id.
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
		{Kind: core.LLMStreamText, Text: "rou"},
		{Kind: core.LLMStreamText, Text: "ting"},
		{Kind: core.LLMStreamToolInput, Text: `{"route":`, Tool: "select_route", ToolCallID: "tc_1"},
		{Kind: core.LLMStreamToolInput, Text: `"billing"}`, Tool: "select_route", ToolCallID: "tc_1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events =\n%+v\nwant\n%+v", got, want)
	}
}

// TestStreamUsageIsAssignedNotSummed guards the defensive read: the SDK's
// accumulator adds usage across chunks, so an OpenAI-compatible server that
// repeats it on every chunk would report a multiple of the real figure. This
// connector advertises baseURL for exactly those servers.
func TestStreamUsageIsAssignedNotSummed(t *testing.T) {
	repeated := `{"id":"c1","object":"chat.completion.chunk","created":1,"model":"m",
		"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],
		"usage":{"prompt_tokens":5,"completion_tokens":7}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(t, w, []string{repeated, repeated, repeated})
	}))
	defer srv.Close()

	resp, err := streamConnector(t, srv.URL).Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	want := core.LLMUsage{InputTokens: 5, OutputTokens: 7}
	if resp.Usage == nil || *resp.Usage != want {
		t.Errorf("usage = %+v, want %+v — the last report wins, it is not a running total", resp.Usage, want)
	}
}
