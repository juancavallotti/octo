package anthropic

import (
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

// serverToolResponse is a turn that searched the web before answering: the
// search call and its result sit between two text blocks.
const serverToolResponse = `{
	"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-sonnet-4-6",
	"content": [
		{"type": "text", "text": "Let me look. "},
		{"type": "server_tool_use", "id": "srvtoolu_1", "name": "web_search", "input": {"query": "octo release"}},
		{"type": "web_search_tool_result", "tool_use_id": "srvtoolu_1", "content": [
			{"type": "web_search_result", "url": "https://example.com", "title": "Example",
			 "encrypted_content": "ENC", "page_age": "1 day"}
		]},
		{"type": "text", "text": "It shipped."}
	],
	"stop_reason": "end_turn",
	"usage": {"input_tokens": 10, "output_tokens": 4}
}`

func TestServerToolTranslation(t *testing.T) {
	cases := []struct {
		name string
		tool core.LLMProviderTool
		want string
	}{
		{
			name: "latest web search",
			tool: core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{
				"maxUses": 3, "allowedDomains": []any{"example.com"},
				"userLocation": map[string]any{"country": "US"},
			}},
			want: `{"max_uses":3,"allowed_domains":["example.com"],"user_location":{"country":"US",` +
				`"type":"approximate"},"name":"web_search","type":"web_search_20260209"}`,
		},
		{
			name: "basic web search",
			tool: core.LLMProviderTool{Type: toolWebSearch, Settings: types.Settings{"version": "basic"}},
			want: `{"name":"web_search","type":"web_search_20250305"}`,
		},
		{
			name: "web fetch with citations",
			tool: core.LLMProviderTool{Type: toolWebFetch, Settings: types.Settings{
				"citations": true, "maxContentTokens": 5000,
			}},
			want: `{"max_content_tokens":5000,"citations":{"enabled":true},"name":"web_fetch","type":"web_fetch_20260209"}`,
		},
		{
			name: "code execution",
			tool: core.LLMProviderTool{Type: toolCodeExecution},
			want: `{"name":"code_execution","type":"code_execution_20260120"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tools, err := toServerTools([]core.LLMProviderTool{tc.tool})
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			got, err := json.Marshal(tools[0])
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("tool =\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestValidateProviderToolsRefusals(t *testing.T) {
	cases := []struct {
		name  string
		tools []core.LLMProviderTool
		want  string
	}{
		{"unknown type", []core.LLMProviderTool{{Type: "image_generation"}}, "not one of: web_search, web_fetch, code_execution"},
		{"unknown setting", []core.LLMProviderTool{{Type: toolWebSearch, Settings: types.Settings{"maxUse": 1}}}, `"web_search"`},
		{"both domain lists", []core.LLMProviderTool{{Type: toolWebFetch, Settings: types.Settings{
			"allowedDomains": []any{"a.com"}, "blockedDomains": []any{"b.com"},
		}}}, "cannot both be set"},
		{"bad version", []core.LLMProviderTool{{Type: toolWebSearch, Settings: types.Settings{"version": "v2"}}}, "version must be"},
		{"negative cap", []core.LLMProviderTool{{Type: toolWebSearch, Settings: types.Settings{"maxUses": -1}}}, "must not be negative"},
		{"settings on code execution", []core.LLMProviderTool{{Type: toolCodeExecution, Settings: types.Settings{"x": 1}}}, `"code_execution"`},
	}
	c := &Connector{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateProviderTools(tc.tools)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
	// The API accepts an explicit code_execution beside a dynamic-filtering web
	// tool, so the connector does too.
	if err := c.ValidateProviderTools([]core.LLMProviderTool{
		{Type: toolWebSearch}, {Type: toolCodeExecution},
	}); err != nil {
		t.Errorf("code execution beside a latest web tool should validate: %v", err)
	}
}

// startAgainst starts a connector pointed at srv.
func startAgainst(t *testing.T, srv *httptest.Server) *Connector {
	t.Helper()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "claude",
		Settings: types.Settings{"apiKey": "sk-test", "baseURL": srv.URL},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return c
}

// The server tool exchange cannot be expressed by the portable fields, so the
// turn is kept opaque, and echoing Raw sends it back block for block.
func TestServerToolTurnRoundTripsOpaque(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, serverToolResponse)
	}))
	defer srv.Close()
	c := startAgainst(t, srv)

	req := core.LLMRequest{
		Messages:      []core.LLMMessage{{Role: core.LLMRoleUser, Text: "did octo ship?"}},
		ProviderTools: []core.LLMProviderTool{{Type: toolWebSearch}},
	}
	resp, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != "Let me look. It shipped." {
		t.Errorf("text = %q", resp.Text)
	}
	if resp.Raw.Opaque == nil || resp.Raw.Opaque.Provider != core.ProviderAnthropic {
		t.Fatalf("raw turn not kept opaque: %+v", resp.Raw.Opaque)
	}
	tools, _ := bodies[0]["tools"].([]any)
	if len(tools) != 1 || tools[0].(map[string]any)["type"] != "web_search_20260209" {
		t.Errorf("request tools = %v, want web_search_20260209", bodies[0]["tools"])
	}

	req.Messages = append(req.Messages, resp.Raw)
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	echoed := assistantContent(t, bodies[1])
	wantTypes := []string{"text", "server_tool_use", "web_search_tool_result", "text"}
	if len(echoed) != len(wantTypes) {
		t.Fatalf("echoed %d blocks, want %d: %v", len(echoed), len(wantTypes), echoed)
	}
	for i, want := range wantTypes {
		if echoed[i]["type"] != want {
			t.Errorf("echoed block %d type = %v, want %s", i, echoed[i]["type"], want)
		}
	}
	result, _ := echoed[2]["content"].([]any)
	if len(result) != 1 || result[0].(map[string]any)["encrypted_content"] != "ENC" {
		t.Errorf("search result not echoed verbatim: %v", echoed[2]["content"])
	}
}

// A turn written by another provider is replayed from its portable fields.
func TestForeignOpaqueTurnFallsBackToPortableFields(t *testing.T) {
	m := core.LLMMessage{
		Role:   core.LLMRoleAssistant,
		Text:   "hello",
		Opaque: &core.LLMOpaqueTurn{Provider: core.ProviderOpenAI, Content: json.RawMessage(`[{"type":"web_search_call"}]`)},
	}
	if m.OpaqueFor(core.ProviderAnthropic) != nil {
		t.Fatal("another provider's opaque turn should not be ours to echo")
	}
	blocks := assistantBlocks(m)
	if len(blocks) != 1 || blocks[0].OfText == nil || blocks[0].OfText.Text != "hello" {
		t.Errorf("blocks = %+v, want the portable text", blocks)
	}
}

func TestPortableTurnStaysTransparent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"claude-sonnet-4-6",
			"content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	resp, err := startAgainst(t, srv).Complete(context.Background(), core.LLMRequest{
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Raw.Opaque != nil {
		t.Errorf("a plain text turn should not be kept opaque: %s", resp.Raw.Opaque.Content)
	}
}

// A paused turn is resumed with the content so far as the trailing assistant
// message, and the rounds fold into one turn.
func TestPauseTurnIsResumed(t *testing.T) {
	paused := `{"id":"m1","type":"message","role":"assistant","model":"claude-sonnet-4-6",
		"content":[{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{"query":"q"}}],
		"stop_reason":"pause_turn","usage":{"input_tokens":10,"output_tokens":2}}`
	done := `{"id":"m2","type":"message","role":"assistant","model":"claude-sonnet-4-6",
		"content":[{"type":"web_search_tool_result","tool_use_id":"srvtoolu_1","content":[]},
		           {"type":"text","text":"answer"}],
		"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":3}}`
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			_, _ = io.WriteString(w, paused)
			return
		}
		_, _ = io.WriteString(w, done)
	}))
	defer srv.Close()

	resp, err := startAgainst(t, srv).Complete(context.Background(), core.LLMRequest{
		Messages:      []core.LLMMessage{{Role: core.LLMRoleUser, Text: "q"}},
		ProviderTools: []core.LLMProviderTool{{Type: toolWebSearch}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(bodies))
	}
	resumed := assistantContent(t, bodies[1])
	if len(resumed) != 1 || resumed[0]["type"] != "server_tool_use" {
		t.Errorf("resume request trailing assistant = %v, want the paused server_tool_use", resumed)
	}
	if resp.Text != "answer" || resp.StopReason != core.LLMStopEndTurn {
		t.Errorf("resp = %q / %q", resp.Text, resp.StopReason)
	}
	if resp.Usage.InputTokens != 22 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want the rounds summed", resp.Usage)
	}
	var blocks []map[string]any
	_ = json.Unmarshal(resp.Raw.Opaque.Content, &blocks)
	if len(blocks) != 3 {
		t.Errorf("opaque turn has %d blocks, want all 3 rounds' content", len(blocks))
	}
}

// A paused streamed turn: the server tool's input arrives as custom events, and
// the resumed round's indexes continue after the first round's.
func TestStreamedServerToolTurn(t *testing.T) {
	first := []frame{
		{"message_start", `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant",
			"model":"claude-sonnet-4-6","content":[],"stop_reason":null,"usage":{"input_tokens":5,"output_tokens":1}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,
			"content_block":{"type":"server_tool_use","id":"srvtoolu_1","name":"web_search","input":{}}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,
			"delta":{"type":"input_json_delta","partial_json":"{\"query\":\"q\"}"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"pause_turn"},"usage":{"output_tokens":2}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	second := []frame{
		{"message_start", `{"type":"message_start","message":{"id":"m2","type":"message","role":"assistant",
			"model":"claude-sonnet-4-6","content":[],"stop_reason":null,"usage":{"input_tokens":6,"output_tokens":1}}}`},
		{"content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`},
		{"content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"found"}}`},
		{"content_block_stop", `{"type":"content_block_stop","index":0}`},
		{"message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`},
		{"message_stop", `{"type":"message_stop"}`},
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			writeSSE(t, w, first)
			return
		}
		writeSSE(t, w, second)
	}))
	defer srv.Close()

	var events []core.LLMStreamEvent
	resp, err := startAgainst(t, srv).Stream(context.Background(), core.LLMRequest{
		Messages:      []core.LLMMessage{{Role: core.LLMRoleUser, Text: "q"}},
		ProviderTools: []core.LLMProviderTool{{Type: toolWebSearch}},
	}, func(ev core.LLMStreamEvent) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %+v, want 2", events)
	}
	if ev := events[0]; ev.Kind != core.LLMStreamCustom || ev.Name != "server_tool_use" || ev.Tool != "web_search" {
		t.Errorf("server tool input event = %+v, want custom server_tool_use for web_search", ev)
	}
	if ev := events[1]; ev.Kind != core.LLMStreamText || ev.Index != 1 {
		t.Errorf("resumed text event = %+v, want text at index 1", ev)
	}
	if resp.Text != "found" || resp.Raw.Opaque == nil {
		t.Errorf("resp text = %q, opaque = %v", resp.Text, resp.Raw.Opaque)
	}
}

// dynamicFilteringResponse carries the blocks a latest web tool produces when it
// filters results with code, in the shapes the API returned them on a live run.
const dynamicFilteringResponse = `{"id":"m","type":"message","role":"assistant","model":"claude-haiku-5-5",
	"content":[
		{"type":"server_tool_use","id":"srvtoolu_1","name":"code_execution","input":{"code":"print(1)"}},
		{"type":"code_execution_tool_result","tool_use_id":"srvtoolu_1",
		 "content":{"type":"encrypted_code_execution_result","encrypted_stdout":"ENC","stderr":"","return_code":0,"content":[]}},
		{"type":"server_tool_use","id":"srvtoolu_2","name":"web_fetch","input":{"url":"https://go.dev"},
		 "caller":{"type":"code_execution_20260120","tool_id":"srvtoolu_1"}},
		{"type":"web_fetch_tool_result","tool_use_id":"srvtoolu_2",
		 "content":{"type":"web_fetch_tool_result_error","error_code":"url_not_in_prior_context"},
		 "caller":{"type":"code_execution_20260120","tool_id":"srvtoolu_1"}},
		{"type":"text","text":"Done."}
	],
	"stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`

// The echo sends every block back exactly as received: the SDK's request types
// drop an encrypted code execution result, a web fetch result's content and a
// block's caller, and the API rejects the turn without them.
func TestServerToolTurnEchoesBlocksByteForByte(t *testing.T) {
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		bodies = append(bodies, raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, dynamicFilteringResponse)
	}))
	defer srv.Close()
	c := startAgainst(t, srv)

	req := core.LLMRequest{
		Messages:      []core.LLMMessage{{Role: core.LLMRoleUser, Text: "latest go?"}},
		ProviderTools: []core.LLMProviderTool{{Type: toolWebSearch}, {Type: toolWebFetch}},
	}
	resp, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	req.Messages = append(req.Messages, resp.Raw)
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatalf("second turn: %v", err)
	}

	var sent struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(bodies[1], &sent); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	var got, want struct {
		Content []any `json:"content"`
	}
	_ = json.Unmarshal([]byte(dynamicFilteringResponse), &want)
	if err := json.Unmarshal([]byte(`{"content":`+string(sent.Messages[1].Content)+`}`), &got); err != nil {
		t.Fatalf("decode echoed content: %v", err)
	}
	if !reflect.DeepEqual(got.Content, want.Content) {
		t.Errorf("echoed content differs from the response:\n got  %v\n want %v", got.Content, want.Content)
	}
}
