package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func TestStartRequiresAPIKey(t *testing.T) {
	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "claude",
		Type:     "llm-anthropic",
		Settings: types.Settings{},
	})
	if err == nil {
		t.Fatal("expected error when apiKey is missing")
	}
	if !strings.Contains(err.Error(), "apiKey is required") {
		t.Errorf("error = %v, want apiKey-required message", err)
	}
}

func TestStartAppliesDefaults(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "claude",
		Settings: types.Settings{"apiKey": "sk-test"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if c.model != defaultModel {
		t.Errorf("model = %q, want default %q", c.model, defaultModel)
	}
	if c.maxTokens != defaultMaxTokens {
		t.Errorf("maxTokens = %d, want default %d", c.maxTokens, defaultMaxTokens)
	}
}

func TestMapStopReason(t *testing.T) {
	cases := map[string]core.LLMStopReason{
		"tool_use":      core.LLMStopToolUse,
		"max_tokens":    core.LLMStopMaxTokens,
		"refusal":       core.LLMStopRefusal,
		"end_turn":      core.LLMStopEndTurn,
		"stop_sequence": core.LLMStopEndTurn,
		"pause_turn":    core.LLMStopEndTurn,
	}
	for wire, want := range cases {
		if got := mapStopReason(wire); got != want {
			t.Errorf("mapStopReason(%q) = %q, want %q", wire, got, want)
		}
	}
}

func TestToToolChoice(t *testing.T) {
	if _, ok := toToolChoice(core.LLMToolChoice{}); ok {
		t.Error("auto (zero) mode should signal unset")
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceAny}); !ok || c.OfAny == nil {
		t.Error("any mode should set OfAny")
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceNone}); !ok || c.OfNone == nil {
		t.Error("none mode should set OfNone")
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "pick"}); !ok || c.OfTool == nil {
		t.Error("tool mode should set OfTool")
	}
}

func TestToToolsDecodesSchema(t *testing.T) {
	tools, err := toTools([]core.LLMTool{{
		Name:        "select_route",
		Description: "pick a route",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"route":{"type":"string"}},"required":["route"]}`),
	}})
	if err != nil {
		t.Fatalf("toTools: %v", err)
	}
	if len(tools) != 1 || tools[0].OfTool == nil {
		t.Fatalf("expected one tool, got %+v", tools)
	}
	tp := tools[0].OfTool
	if tp.Name != "select_route" || tp.Description.Value != "pick a route" {
		t.Errorf("tool name/description not set: %+v", tp)
	}
	if len(tp.InputSchema.Required) != 1 || tp.InputSchema.Required[0] != "route" {
		t.Errorf("schema required not parsed: %+v", tp.InputSchema)
	}
}

func TestToMessagesRoles(t *testing.T) {
	msgs, err := toMessages([]core.LLMMessage{
		{Role: core.LLMRoleUser, Text: "hi"},
		{Role: core.LLMRoleAssistant, Text: "ok", ToolCalls: []core.LLMToolCall{
			{ID: "tu_1", Name: "look", Input: json.RawMessage(`{"q":"x"}`)},
		}},
		{Role: core.LLMRoleTool, ToolResults: []core.LLMToolResult{
			{ToolCallID: "tu_1", Content: `{"ok":true}`},
		}},
	})
	if err != nil {
		t.Fatalf("toMessages: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if _, err := toMessages([]core.LLMMessage{{Role: "bogus"}}); err == nil {
		t.Error("expected error for unknown role")
	}
}

// TestCompleteEndToEnd drives Complete against a canned Anthropic response served
// by an httptest server, proving request marshaling and response translation.
func TestCompleteEndToEnd(t *testing.T) {
	const cannedResponse = `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-opus-4-8",
		"content": [
			{"type": "text", "text": "routing"},
			{"type": "tool_use", "id": "tu_1", "name": "select_route", "input": {"route": "billing"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 5, "output_tokens": 3}
	}`

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, cannedResponse)
	}))
	defer srv.Close()

	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name:     "claude",
		Settings: types.Settings{"apiKey": "sk-test", "baseURL": srv.URL},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	resp, err := c.Complete(context.Background(), core.LLMRequest{
		System:   "you route tickets",
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "billing question"}},
		Tools: []core.LLMTool{{
			Name:        "select_route",
			Description: "pick a route",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "select_route"},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if resp.Text != "routing" {
		t.Errorf("text = %q, want routing", resp.Text)
	}
	if resp.StopReason != core.LLMStopToolUse {
		t.Errorf("stop reason = %q, want tool_use", resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	call := resp.ToolCalls[0]
	if call.ID != "tu_1" || call.Name != "select_route" {
		t.Errorf("tool call id/name = %+v", call)
	}
	var input struct {
		Route string `json:"route"`
	}
	if err := json.Unmarshal(call.Input, &input); err != nil || input.Route != "billing" {
		t.Errorf("tool call input = %s (route=%q, err=%v)", call.Input, input.Route, err)
	}

	// The request the server received should carry the system prompt and the
	// forced tool choice.
	if gotBody["system"] == nil {
		t.Error("request missing system prompt")
	}
	if tc, ok := gotBody["tool_choice"].(map[string]any); !ok || tc["type"] != "tool" {
		t.Errorf("request tool_choice = %v, want type=tool", gotBody["tool_choice"])
	}
}
