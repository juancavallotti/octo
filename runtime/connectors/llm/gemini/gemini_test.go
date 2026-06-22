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

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

func TestStartRequiresAPIKey(t *testing.T) {
	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{Name: "gem", Settings: types.Settings{}})
	if err == nil || !strings.Contains(err.Error(), "apiKey is required") {
		t.Fatalf("expected apiKey-required error, got %v", err)
	}
}

func TestMapFinishReason(t *testing.T) {
	// Function calls always mean tool-use, regardless of the reported reason.
	if got := mapFinishReason(genai.FinishReasonStop, true); got != core.LLMStopToolUse {
		t.Errorf("with calls = %q, want tool_use", got)
	}
	cases := map[genai.FinishReason]core.LLMStopReason{
		genai.FinishReasonStop:      core.LLMStopEndTurn,
		genai.FinishReasonMaxTokens: core.LLMStopMaxTokens,
		genai.FinishReasonSafety:    core.LLMStopRefusal,
	}
	for reason, want := range cases {
		if got := mapFinishReason(reason, false); got != want {
			t.Errorf("mapFinishReason(%q) = %q, want %q", reason, got, want)
		}
	}
}

func TestToToolConfig(t *testing.T) {
	if _, ok := toToolConfig(core.LLMToolChoice{}); ok {
		t.Error("auto (zero) mode should leave the config unset")
	}
	if tc, ok := toToolConfig(core.LLMToolChoice{Mode: core.LLMToolChoiceAny}); !ok ||
		tc.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny {
		t.Errorf("any mode should map to ANY: %+v", tc)
	}
	if tc, ok := toToolConfig(core.LLMToolChoice{Mode: core.LLMToolChoiceNone}); !ok ||
		tc.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeNone {
		t.Errorf("none mode should map to NONE: %+v", tc)
	}
	tc, ok := toToolConfig(core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "pick"})
	if !ok || tc.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny ||
		len(tc.FunctionCallingConfig.AllowedFunctionNames) != 1 {
		t.Errorf("tool mode should restrict to the named function: %+v", tc)
	}
}

func TestResponseMap(t *testing.T) {
	if m := responseMap(core.LLMToolResult{Content: `{"a":1}`}); m["a"] != float64(1) {
		t.Errorf("object content should pass through: %#v", m)
	}
	if m := responseMap(core.LLMToolResult{Content: `[1,2]`}); m["result"] == nil {
		t.Errorf("non-object content should be wrapped under result: %#v", m)
	}
	if m := responseMap(core.LLMToolResult{Content: "boom", IsError: true}); m["error"] != "boom" {
		t.Errorf("error result should be reported under error: %#v", m)
	}
}

func TestToContentsRoles(t *testing.T) {
	contents, err := toContents([]core.LLMMessage{
		{Role: core.LLMRoleUser, Text: "hi"},
		{Role: core.LLMRoleAssistant, ToolCalls: []core.LLMToolCall{
			{ID: "look", Name: "look", Input: json.RawMessage(`{"q":"x"}`)},
		}},
		{Role: core.LLMRoleTool, ToolResults: []core.LLMToolResult{{ToolCallID: "look", Content: `{"ok":true}`}}},
	})
	if err != nil {
		t.Fatalf("toContents: %v", err)
	}
	if len(contents) != 3 {
		t.Fatalf("contents = %d, want 3", len(contents))
	}
	if contents[1].Role != genai.RoleModel || contents[1].Parts[0].FunctionCall == nil {
		t.Errorf("assistant turn should carry a function call: %+v", contents[1])
	}
	if contents[2].Parts[0].FunctionResponse == nil || contents[2].Parts[0].FunctionResponse.Name != "look" {
		t.Errorf("tool turn should carry a named function response: %+v", contents[2])
	}
	if _, err := toContents([]core.LLMMessage{{Role: "bogus"}}); err == nil {
		t.Error("expected error for unknown role")
	}
}

// TestCompleteEndToEnd drives Complete against a canned Gemini response served by
// an httptest server, proving request marshaling and response translation,
// including the STOP-with-function-call case.
func TestCompleteEndToEnd(t *testing.T) {
	const cannedResponse = `{
		"candidates": [{
			"content": {
				"role": "model",
				"parts": [
					{"text": "routing"},
					{"functionCall": {"name": "select_route", "args": {"route": "billing"}}}
				]
			},
			"finishReason": "STOP"
		}]
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
		Name: "gem", Settings: types.Settings{"apiKey": "test", "baseURL": srv.URL},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	resp, err := c.Complete(context.Background(), core.LLMRequest{
		System:   "you route tickets",
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "billing question"}},
		Tools: []core.LLMTool{{
			Name: "select_route", Description: "pick", InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
		ToolChoice: core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "select_route"},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if resp.Text != "routing" || resp.StopReason != core.LLMStopToolUse {
		t.Errorf("text/stop = %q/%q", resp.Text, resp.StopReason)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(resp.ToolCalls))
	}
	call := resp.ToolCalls[0]
	var input struct {
		Route string `json:"route"`
	}
	if call.Name != "select_route" || call.ID != "select_route" ||
		json.Unmarshal(call.Input, &input) != nil || input.Route != "billing" {
		t.Errorf("tool call = %+v (route=%q)", call, input.Route)
	}

	// The request should carry the system instruction and a function-calling config.
	if gotBody["systemInstruction"] == nil {
		t.Error("request missing systemInstruction")
	}
	if gotBody["toolConfig"] == nil {
		t.Error("request missing toolConfig")
	}
}
