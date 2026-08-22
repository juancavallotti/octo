package openrouter

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

// writeSSE serves Chat Completions frames. They carry data only — the format
// names no event type — and end with the [DONE] sentinel the SDK stops on.
func writeSSE(t *testing.T, w http.ResponseWriter, frames []string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	for _, f := range frames {
		var buf bytes.Buffer
		if err := json.Compact(&buf, []byte(f)); err != nil {
			t.Errorf("fixture frame is not valid JSON: %v", err)
			return
		}
		_, _ = io.WriteString(w, "data: "+buf.String()+"\n\n")
	}
	_, _ = io.WriteString(w, "data: [DONE]\n\n")
}

// The finished turn, as the blocking call returns it. The streamed frames below
// build the same turn, so the equality test compares two translations of one
// answer rather than two hand-written fixtures.
const finishedTurn = `{
	"id":"gen_1","object":"chat.completion","created":1,"model":"anthropic/claude-sonnet-4.5",
	"choices":[{
		"index":0,"finish_reason":"tool_calls",
		"message":{
			"role":"assistant","content":"routing","refusal":null,
			"reasoning":"billing then",
			"reasoning_details":[{"type":"reasoning.text","text":"billing then","signature":"sig-1"}],
			"tool_calls":[{"index":0,"id":"tc_1","type":"function",
				"function":{"name":"select_route","arguments":"{\"route\":\"billing\"}"}}]
		}
	}],
	"usage":{"prompt_tokens":5,"completion_tokens":30,"total_tokens":35,"cost":0.00042,
		"prompt_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},
		"completion_tokens_details":{"reasoning_tokens":20}}
}`

// The same turn streamed: reasoning fragments, then answer fragments, then a tool
// call announced once with its id and name and extended with argument fragments,
// and finally the choice-less chunk that stream_options asks for and that carries
// the usage.
var streamedFrames = []string{
	`{"id":"gen_1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[{"index":0,"delta":{"role":"assistant","reasoning":"billing "},"finish_reason":null}]}`,
	`{"id":"gen_1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[{"index":0,"delta":{"reasoning":"then",
			"reasoning_details":[{"type":"reasoning.text","text":"billing then","signature":"sig-1"}]},
			"finish_reason":null}]}`,
	`{"id":"gen_1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[{"index":0,"delta":{"content":"rou"},"finish_reason":null}]}`,
	`{"id":"gen_1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[{"index":0,"delta":{"content":"ting"},"finish_reason":null}]}`,
	`{"id":"gen_1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"tc_1","type":"function",
			"function":{"name":"select_route","arguments":"{\"route\":"}}]},"finish_reason":null}]}`,
	`{"id":"gen_1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,
			"function":{"arguments":"\"billing\"}"}}]},"finish_reason":"tool_calls"}]}`,
	`{"id":"gen_1","object":"chat.completion.chunk","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[],
		"usage":{"prompt_tokens":5,"completion_tokens":30,"total_tokens":35,"cost":0.00042,
			"prompt_tokens_details":{"cached_tokens":3,"cache_write_tokens":2},
			"completion_tokens_details":{"reasoning_tokens":20}}}`,
}

// turnServer answers the same turn in whichever shape the request asks for.
func turnServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if stream, _ := req["stream"].(bool); stream {
			writeSSE(t, w, streamedFrames)
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
		Name:     "router",
		Settings: types.Settings{"apiKey": "sk-or-test", "baseURL": url},
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
	// Guard against the comparison passing because both are empty.
	if streamed.Text != "routing" || len(streamed.ToolCalls) != 1 || streamed.Usage == nil {
		t.Fatalf("fixture did not translate: %+v", streamed)
	}
	// The reasoning has to survive the streamed path too, or a tool loop on a
	// thinking model breaks on its second turn with nothing to echo back.
	if len(streamed.Raw.Thinking) != 1 || len(streamed.Raw.Thinking[0].Redacted) == 0 {
		t.Errorf("streamed turn lost its reasoning: %+v", streamed.Raw.Thinking)
	}
}

// TestStreamEmitsCanonicalEvents pins the mapping, in particular that every
// tool_input fragment names its call even though the name and id are announced
// only once, on the chunk that opens it.
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
		{Kind: core.LLMStreamThinking, Text: "billing "},
		{Kind: core.LLMStreamThinking, Text: "then"},
		{Kind: core.LLMStreamText, Text: "rou"},
		{Kind: core.LLMStreamText, Text: "ting"},
		{Kind: core.LLMStreamToolInput, Text: `{"route":`, Tool: "select_route", ToolCallID: "tc_1"},
		{Kind: core.LLMStreamToolInput, Text: `"billing"}`, Tool: "select_route", ToolCallID: "tc_1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events =\n%+v\nwant\n%+v", got, want)
	}
}

// TestStreamStopsWhenHandlerErrors pins that a caller abandoning a turn stops the
// stream rather than being ignored to its end.
func TestStreamStopsWhenHandlerErrors(t *testing.T) {
	srv := turnServer(t)
	defer srv.Close()

	seen := 0
	_, err := streamConnector(t, srv.URL).Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error {
			seen++
			return io.ErrUnexpectedEOF
		})
	if err == nil {
		t.Fatal("expected the handler's error to be returned")
	}
	if seen != 1 {
		t.Errorf("handler called %d times, want it to stop the stream after the first", seen)
	}
}

// TestStreamUsageIsLatchedNotSummed pins the idiom: the usage object arrives
// whole, on its own chunk. Summing it would multiply every count by the number of
// chunks that carried one, and the cost with them.
func TestStreamUsageIsLatchedNotSummed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(t, w, []string{
			`{"id":"g","object":"chat.completion.chunk","created":1,"model":"m",
				"choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],
				"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"cost":0.001}}`,
			`{"id":"g","object":"chat.completion.chunk","created":1,"model":"m","choices":[],
				"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"cost":0.001}}`,
		})
	}))
	defer srv.Close()

	resp, err := streamConnector(t, srv.URL).Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 4 {
		t.Errorf("usage = %+v, want it latched at 10/4 rather than summed", resp.Usage)
	}
	if resp.Usage.ReportedCostUSD == nil || *resp.Usage.ReportedCostUSD != 0.001 {
		t.Errorf("cost = %v, want it latched at 0.001", resp.Usage.ReportedCostUSD)
	}
}

// TestStreamWithoutAnyChunksFails pins that an empty stream is an error rather
// than a turn in which the model answered nothing.
func TestStreamWithoutAnyChunksFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	_, err := streamConnector(t, srv.URL).Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error { return nil })
	if err == nil {
		t.Fatal("expected an error when the stream carries no chunks")
	}
}

// A turn with no tool calls has to fold to the same nil slice the blocking path
// produces, or the equality the streaming contract promises fails on the most
// ordinary turn there is.
func TestStreamWithNoToolCallsMatchesComplete(t *testing.T) {
	const plainTurn = `{"id":"g","object":"chat.completion","created":1,"model":"m",
		"choices":[{"index":0,"finish_reason":"stop",
			"message":{"role":"assistant","content":"hello","refusal":null}}],
		"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if stream, _ := req["stream"].(bool); stream {
			writeSSE(t, w, []string{
				`{"id":"g","object":"chat.completion.chunk","created":1,"model":"m",
					"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"stop"}]}`,
				`{"id":"g","object":"chat.completion.chunk","created":1,"model":"m","choices":[],
					"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`,
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, plainTurn)
	}))
	defer srv.Close()
	c := streamConnector(t, srv.URL)

	req := core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}}
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
	if streamed.Text != "hello" || streamed.StopReason != core.LLMStopEndTurn {
		t.Fatalf("fixture did not translate: %+v", streamed)
	}
}
