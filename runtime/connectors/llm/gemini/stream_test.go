package gemini

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

// writeSSE serves Gemini-format chunks.
//
// An event: line must NOT appear here — the opposite of the Anthropic connector's
// fixtures. This SDK's parser cuts the whole frame at its first colon and rejects
// any prefix but "data", so a frame naming its event fails the stream outright.
// The payload is compacted for the same reason as elsewhere: a data field ends at
// its newline.
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
}

// The same turn in both shapes. The streamed one splits both the thought and the
// answer mid-run, which is what Gemini actually does and what the fold has to undo.
var (
	streamedChunks = []string{
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"the user ","thought":true}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"wants billing","thought":true,
			"thoughtSignature":"c2lnLWFiYw=="}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"rou"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[{"text":"ting"}]}}]}`,
		`{"candidates":[{"content":{"role":"model","parts":[
			{"functionCall":{"name":"select_route","args":{"route":"billing"}}}]},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":10,
				"thoughtsTokenCount":20,"cachedContentTokenCount":3}}`,
	}

	blockingTurn = `{"candidates":[{"content":{"role":"model","parts":[
		{"text":"the user wants billing","thought":true,"thoughtSignature":"c2lnLWFiYw=="},
		{"text":"routing"},
		{"functionCall":{"name":"select_route","args":{"route":"billing"}}}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":10,
			"thoughtsTokenCount":20,"cachedContentTokenCount":3}}`
)

// turnServer answers either shape, choosing by the endpoint the SDK calls.
func turnServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "streamGenerateContent") {
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
		Name:     "gem",
		Settings: types.Settings{"apiKey": "k", "baseURL": url, "thinking": thinkingDynamic},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return c
}

// TestStreamMatchesComplete is the property the whole feature rests on: for the
// same turn, streaming returns exactly what the blocking call would have. It is
// the sharpest test of the three providers, because genai ships no accumulator and
// this fold is entirely ours — including rejoining runs Gemini split mid-word.
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
	if streamed.Text != "routing" || len(streamed.Raw.Thinking) != 1 || streamed.Usage == nil {
		t.Fatalf("fixture did not translate: %+v", streamed)
	}
}

// TestStreamEmitsCanonicalEvents pins the mapping, including that a function call
// is reported as one tool_input carrying its whole argument JSON — Gemini does not
// fragment arguments, but a consumer concatenating tool_input still gets valid
// JSON, the same as on the other two providers.
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
		{Kind: core.LLMStreamThinking, Text: "the user ", Index: 0},
		{Kind: core.LLMStreamThinking, Text: "wants billing", Index: 0},
		{Kind: core.LLMStreamText, Text: "rou", Index: 1},
		{Kind: core.LLMStreamText, Text: "ting", Index: 1},
		{
			Kind: core.LLMStreamToolInput, Text: `{"route":"billing"}`,
			Tool: "select_route", ToolCallID: "select_route-0", Index: 2,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events =\n%+v\nwant\n%+v", got, want)
	}
}

// TestStreamUsageIsLatchedNotSummed pins the third of the three usage idioms:
// Gemini repeats a running snapshot, so the last one wins. Summing here — the
// idiom the OpenAI accumulator uses — would silently multiply every count.
func TestStreamUsageIsLatchedNotSummed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSSE(t, w, []string{
			`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]}}],
				"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":1}}`,
			`{"candidates":[{"content":{"role":"model","parts":[{"text":" there"}]},"finishReason":"STOP"}],
				"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":3}}`,
		})
	}))
	defer srv.Close()

	resp, err := streamConnector(t, srv.URL).Stream(context.Background(),
		core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}},
		func(core.LLMStreamEvent) error { return nil })
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	want := core.LLMUsage{InputTokens: 5, OutputTokens: 3, PromptTokens: 5}
	if resp.Usage == nil || *resp.Usage != want {
		t.Errorf("usage = %+v, want %+v — the last snapshot wins, counts are not added up", resp.Usage, want)
	}
}
