package gemini

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// codeExecutionResponse is a turn that wrote and ran code before answering.
const codeExecutionResponse = `{
	"candidates": [{
		"content": {"role": "model", "parts": [
			{"executableCode": {"language": "PYTHON", "code": "print(2+2)"}},
			{"codeExecutionResult": {"outcome": "OUTCOME_OK", "output": "4\n"}},
			{"text": "The answer is 4."}
		]},
		"finishReason": "STOP"
	}],
	"usageMetadata": {"promptTokenCount": 5, "candidatesTokenCount": 3, "totalTokenCount": 8},
	"modelVersion": "gemini-3.5-flash"
}`

func TestValidateProviderTools(t *testing.T) {
	c := &Connector{}
	if err := c.ValidateProviderTools([]core.LLMProviderTool{
		{Type: toolGoogleSearch}, {Type: toolCodeExecution}, {Type: toolURLContext},
	}); err != nil {
		t.Fatalf("the whole catalogue should validate: %v", err)
	}
	cases := []struct {
		name string
		tool core.LLMProviderTool
		want string
	}{
		{"unknown type", core.LLMProviderTool{Type: "web_search"}, "not one of: google_search, code_execution, url_context"},
		{"stray setting", core.LLMProviderTool{Type: toolGoogleSearch, Settings: types.Settings{"maxUses": 2}}, `"google_search"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := c.ValidateProviderTools([]core.LLMProviderTool{tc.tool})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// The built-in tools ride beside the function declarations, and a code
// execution turn is echoed part for part.
func TestCodeExecutionTurnRoundTripsOpaque(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, codeExecutionResponse)
	}))
	defer srv.Close()
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "gem", Settings: types.Settings{"apiKey": "test", "baseURL": srv.URL},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	req := core.LLMRequest{
		Messages:      []core.LLMMessage{{Role: core.LLMRoleUser, Text: "what is 2+2?"}},
		Tools:         []core.LLMTool{{Name: "lookup", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ProviderTools: []core.LLMProviderTool{{Type: toolCodeExecution}, {Type: toolGoogleSearch}},
	}
	resp, err := c.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.Text != "The answer is 4." || resp.Raw.Opaque == nil {
		t.Fatalf("text = %q, opaque = %v", resp.Text, resp.Raw.Opaque)
	}
	tools, _ := bodies[0]["tools"].([]any)
	if len(tools) != 3 {
		t.Fatalf("tools = %v, want functions, code execution and search", bodies[0]["tools"])
	}
	if _, ok := tools[1].(map[string]any)["codeExecution"]; !ok {
		t.Errorf("second tool = %v, want codeExecution", tools[1])
	}
	if _, ok := tools[2].(map[string]any)["googleSearch"]; !ok {
		t.Errorf("third tool = %v, want googleSearch", tools[2])
	}
	toolConfig, _ := bodies[0]["toolConfig"].(map[string]any)
	if toolConfig["includeServerSideToolInvocations"] != true {
		t.Errorf("toolConfig = %v, want includeServerSideToolInvocations beside function tools", bodies[0]["toolConfig"])
	}

	req.Messages = append(req.Messages, resp.Raw, core.LLMMessage{Role: core.LLMRoleUser, Text: "and 3+3?"})
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	contents, _ := bodies[1]["contents"].([]any)
	model := contents[1].(map[string]any)
	parts, _ := model["parts"].([]any)
	if len(parts) != 3 {
		t.Fatalf("echoed parts = %v, want all three", parts)
	}
	if _, ok := parts[0].(map[string]any)["executableCode"]; !ok {
		t.Errorf("first echoed part = %v, want executableCode", parts[0])
	}
}

func TestStreamedCodeExecutionIsCustom(t *testing.T) {
	var events []core.LLMStreamEvent
	err := emitServerToolPart(mustPart(t, `{"executableCode":{"language":"PYTHON","code":"print(1)"}}`), 0,
		func(ev core.LLMStreamEvent) error {
			events = append(events, ev)
			return nil
		})
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if len(events) != 1 || events[0].Kind != core.LLMStreamCustom || events[0].Name != "executable_code" {
		t.Fatalf("events = %+v, want one custom executable_code", events)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(events[0].Text), &payload); err != nil {
		t.Fatalf("payload is not JSON: %v", err)
	}
	if payload["language"] != "PYTHON" || payload["code"] != "print(1)" {
		t.Errorf("payload = %v, want the whole executable code object", payload)
	}
}

// mustPart decodes one wire-format part.
func mustPart(t *testing.T, raw string) *genai.Part {
	t.Helper()
	var p genai.Part
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("decode part: %v", err)
	}
	return &p
}

// A request mixing built-in and function tools gets the server's invocations
// back as toolCall and toolResponse parts, which Gemini expects echoed.
func TestServerToolInvocationsAreOpaque(t *testing.T) {
	parts := []*genai.Part{
		{ToolCall: &genai.ToolCall{ID: "t1", ToolType: "GOOGLE_SEARCH_WEB", Args: map[string]any{"q": "go"}}},
		{ToolResponse: &genai.ToolResponse{ID: "t1", ToolType: "GOOGLE_SEARCH_WEB", Response: map[string]any{"ok": true}}},
		{FunctionCall: &genai.FunctionCall{Name: "lookup"}},
	}
	turn := opaqueTurn(parts)
	if turn == nil {
		t.Fatal("a turn with server tool invocations should be kept opaque")
	}
	echoed, err := opaqueParts(turn.Content)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(echoed) != 3 || echoed[0].ToolCall == nil || echoed[1].ToolResponse == nil {
		t.Fatalf("echoed parts = %+v, want the invocation pair and the call", echoed)
	}
}

// Built-in tools alone need no tool config: the flag is only for the mix.
func TestProviderToolsAloneLeaveToolConfigUnset(t *testing.T) {
	c := &Connector{}
	_, cfg, err := c.request(core.LLMRequest{
		Messages:      []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}},
		ProviderTools: []core.LLMProviderTool{{Type: toolGoogleSearch}},
	})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if cfg.ToolConfig != nil {
		t.Errorf("toolConfig = %+v, want none without function tools", cfg.ToolConfig)
	}
}
