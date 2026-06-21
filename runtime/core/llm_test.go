package core

import (
	"encoding/json"
	"testing"
)

// TestLLMRequestRoundTrip confirms the DTOs hold their raw-JSON fields intact
// through a marshal/unmarshal cycle, which is how connectors carry tool schemas
// and arguments to and from their SDKs.
func TestLLMRequestRoundTrip(t *testing.T) {
	req := LLMRequest{
		System: "you are a router",
		Messages: []LLMMessage{
			{Role: LLMRoleUser, Text: "classify this"},
			{Role: LLMRoleAssistant, ToolCalls: []LLMToolCall{
				{ID: "call_1", Name: "select_route", Input: json.RawMessage(`{"route":"billing"}`)},
			}},
			{Role: LLMRoleTool, ToolResults: []LLMToolResult{
				{ToolCallID: "call_1", Content: `{"ok":true}`},
			}},
		},
		Tools: []LLMTool{
			{Name: "select_route", Description: "pick a route", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
		ToolChoice: LLMToolChoice{Mode: LLMToolChoiceTool, Name: "select_route"},
		MaxTokens:  4096,
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got LLMRequest
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.System != req.System || got.MaxTokens != req.MaxTokens {
		t.Errorf("scalar fields not preserved: %+v", got)
	}
	if got.ToolChoice != req.ToolChoice {
		t.Errorf("tool choice = %+v, want %+v", got.ToolChoice, req.ToolChoice)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(got.Messages))
	}
	if call := got.Messages[1].ToolCalls[0]; call.ID != "call_1" || string(call.Input) != `{"route":"billing"}` {
		t.Errorf("tool call not preserved: %+v", call)
	}
	if res := got.Messages[2].ToolResults[0]; res.ToolCallID != "call_1" || res.Content != `{"ok":true}` {
		t.Errorf("tool result not preserved: %+v", res)
	}
	if schema := got.Tools[0].InputSchema; string(schema) != `{"type":"object"}` {
		t.Errorf("tool schema not preserved: %s", schema)
	}
}

// TestLLMToolChoiceZeroValueIsAuto documents that the zero value of LLMToolChoice
// means "auto" so callers can omit it for plain completions.
func TestLLMToolChoiceZeroValueIsAuto(t *testing.T) {
	var tc LLMToolChoice
	if tc.Mode != LLMToolChoiceAuto {
		t.Errorf("zero ToolChoice mode = %q, want auto (empty)", tc.Mode)
	}
}
