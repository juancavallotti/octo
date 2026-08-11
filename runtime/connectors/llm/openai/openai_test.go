package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/openai/openai-go/v2"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func TestStartRequiresAPIKey(t *testing.T) {
	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{Name: "gpt", Settings: types.Settings{}})
	if err == nil || !strings.Contains(err.Error(), "apiKey is required") {
		t.Fatalf("expected apiKey-required error, got %v", err)
	}
}

func TestStartAppliesDefaultModel(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "gpt", Settings: types.Settings{"apiKey": "sk-test"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if c.model != defaultModel {
		t.Errorf("model = %q, want default %q", c.model, defaultModel)
	}
}

func TestMapFinishReason(t *testing.T) {
	cases := []struct {
		reason, refusal string
		want            core.LLMStopReason
	}{
		{"tool_calls", "", core.LLMStopToolUse},
		{"length", "", core.LLMStopMaxTokens},
		{"content_filter", "", core.LLMStopRefusal},
		{"stop", "", core.LLMStopEndTurn},
		{"stop", "I can't help with that", core.LLMStopRefusal},
	}
	for _, tt := range cases {
		if got := mapFinishReason(tt.reason, tt.refusal); got != tt.want {
			t.Errorf("mapFinishReason(%q,%q) = %q, want %q", tt.reason, tt.refusal, got, tt.want)
		}
	}
}

func TestToToolChoice(t *testing.T) {
	if _, ok := toToolChoice(core.LLMToolChoice{}); ok {
		t.Error("auto (zero) mode should signal unset")
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceAny}); !ok || c.OfAuto.Value != "required" {
		t.Errorf("any mode should map to required: %+v", c)
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceNone}); !ok || c.OfAuto.Value != "none" {
		t.Errorf("none mode should map to none: %+v", c)
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "pick"}); !ok || c.OfFunctionToolChoice == nil {
		t.Errorf("tool mode should set the function tool choice: %+v", c)
	}
}

func TestToToolsDecodesSchema(t *testing.T) {
	tools, err := toTools([]core.LLMTool{{
		Name:        "select_route",
		Description: "pick a route",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"route":{"type":"string"}}}`),
	}})
	if err != nil {
		t.Fatalf("toTools: %v", err)
	}
	if len(tools) != 1 || tools[0].OfFunction == nil {
		t.Fatalf("expected one function tool, got %+v", tools)
	}
	fn := tools[0].OfFunction.Function
	if fn.Name != "select_route" || fn.Description.Value != "pick a route" || fn.Parameters["type"] != "object" {
		t.Errorf("function definition not mapped: %+v", fn)
	}
}

func TestToMessagesRoles(t *testing.T) {
	msgs, err := toMessages(core.LLMRequest{
		System: "be terse",
		Messages: []core.LLMMessage{
			{Role: core.LLMRoleUser, Text: "hi"},
			{Role: core.LLMRoleAssistant, Text: "ok", ToolCalls: []core.LLMToolCall{
				{ID: "t1", Name: "look", Input: json.RawMessage(`{"q":"x"}`)},
			}},
			{Role: core.LLMRoleTool, ToolResults: []core.LLMToolResult{{ToolCallID: "t1", Content: `{"ok":true}`}}},
		},
	})
	if err != nil {
		t.Fatalf("toMessages: %v", err)
	}
	// system + user + assistant + tool = 4
	if len(msgs) != 4 {
		t.Fatalf("messages = %d, want 4 (incl. system)", len(msgs))
	}
	if _, err := toMessages(core.LLMRequest{Messages: []core.LLMMessage{{Role: "bogus"}}}); err == nil {
		t.Error("expected error for unknown role")
	}
}

// TestCompleteEndToEnd drives Complete against a canned OpenAI response served by
// an httptest server, proving request marshaling and response translation.
func TestCompleteEndToEnd(t *testing.T) {
	const cannedResponse = `{
		"id": "chatcmpl-1",
		"object": "chat.completion",
		"created": 1,
		"model": "gpt-4.1",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "routing",
				"tool_calls": [
					{"id": "call_1", "type": "function",
					 "function": {"name": "select_route", "arguments": "{\"route\":\"billing\"}"}}
				]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
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
		Name: "gpt", Settings: types.Settings{"apiKey": "sk-test", "baseURL": srv.URL},
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
	if call.ID != "call_1" || call.Name != "select_route" ||
		json.Unmarshal(call.Input, &input) != nil || input.Route != "billing" {
		t.Errorf("tool call = %+v (route=%q)", call, input.Route)
	}

	// The request should carry the system message (role system) and a function
	// tool_choice.
	if msgs, ok := gotBody["messages"].([]any); !ok || len(msgs) == 0 {
		t.Errorf("request messages missing: %v", gotBody["messages"])
	}
	if tc, ok := gotBody["tool_choice"].(map[string]any); !ok || tc["type"] != "function" {
		t.Errorf("request tool_choice = %v, want type=function", gotBody["tool_choice"])
	}
}

// TestEmbedEndToEnd drives Embed against a canned embeddings response served out
// of index order, proving the request shape (batched input, model, dimensions)
// and that the response is reordered back to request order rather than trusted
// as-is.
func TestEmbedEndToEnd(t *testing.T) {
	const cannedResponse = `{
		"object": "list",
		"model": "text-embedding-3-small",
		"data": [
			{"object": "embedding", "index": 1, "embedding": [0.4, 0.5]},
			{"object": "embedding", "index": 0, "embedding": [0.1, 0.2]}
		],
		"usage": {"prompt_tokens": 4, "total_tokens": 4}
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
		Name: "gpt", Settings: types.Settings{"apiKey": "sk-test", "baseURL": srv.URL},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	resp, err := c.Embed(context.Background(), core.EmbedRequest{
		Model:      "text-embedding-3-small",
		Input:      []string{"first", "second"},
		Dimensions: 2,
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if len(resp.Vectors) != 2 {
		t.Fatalf("vectors = %d, want 2", len(resp.Vectors))
	}
	if resp.Vectors[0][0] != float32(0.1) || resp.Vectors[1][0] != float32(0.4) {
		t.Errorf("vectors not reordered to request order: %+v", resp.Vectors)
	}

	// An embedding call is billed, so what it charged has to survive translation.
	if resp.Usage == nil {
		t.Fatal("embed reported no usage though the response carried it")
	}
	if resp.Usage.InputTokens != 4 {
		t.Errorf("usage.InputTokens = %d, want 4", resp.Usage.InputTokens)
	}
	if resp.Model != "text-embedding-3-small" {
		t.Errorf("model = %q, want the one the provider reported", resp.Model)
	}

	if gotBody["model"] != "text-embedding-3-small" {
		t.Errorf("request model = %v", gotBody["model"])
	}
	if input, ok := gotBody["input"].([]any); !ok || len(input) != 2 {
		t.Errorf("request input = %v, want 2-element array", gotBody["input"])
	}
	if gotBody["dimensions"] != float64(2) {
		t.Errorf("request dimensions = %v, want 2", gotBody["dimensions"])
	}
}

// TestEmbedRequiresInput proves Embed rejects an empty batch before making a
// request, rather than sending OpenAI a call it would reject anyway.
func TestEmbedRequiresInput(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "gpt", Settings: types.Settings{"apiKey": "sk-test"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := c.Embed(context.Background(), core.EmbedRequest{Model: "text-embedding-3-small"}); err == nil {
		t.Error("expected error for empty input")
	}
}

func TestTranslateUsage(t *testing.T) {
	if got := translateUsage(sdk.CompletionUsage{}); got != nil {
		t.Errorf("empty usage = %+v, want nil", got)
	}
	got := translateUsage(sdk.CompletionUsage{
		PromptTokens:            100,
		CompletionTokens:        40,
		CompletionTokensDetails: sdk.CompletionUsageCompletionTokensDetails{ReasoningTokens: 25},
		PromptTokensDetails:     sdk.CompletionUsagePromptTokensDetails{CachedTokens: 30},
	})
	// CompletionTokens already counts reasoning, so OutputTokens passes through.
	want := core.LLMUsage{InputTokens: 100, OutputTokens: 40, ThinkingTokens: 25, CachedTokens: 30}
	if got == nil || *got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestToReasoningEffort(t *testing.T) {
	// Unset, the old "off" and the new "none" all mean no reasoning, and all say
	// so on the wire. Omitting the field instead asks a reasoning model for its
	// own default, which is not none — and a non-none default cannot be combined
	// with function tools on /v1/chat/completions, so an agent on such a model
	// failed its first turn with a 400 naming reasoning_effort.
	for _, in := range []string{"", "off", "none"} {
		if got, err := toReasoningEffort(in); err != nil || string(got) != "none" {
			t.Errorf("toReasoningEffort(%q) = %q (err %v), want none", in, got, err)
		}
	}
	// The old behaviour is still reachable, for a model that rejects an explicit
	// none.
	if got, err := toReasoningEffort("default"); err != nil || got != "" {
		t.Errorf(`toReasoningEffort("default") = %q (err %v), want the field omitted`, got, err)
	}
	for _, in := range []string{"minimal", "low", "medium", "high"} {
		if got, err := toReasoningEffort(in); err != nil || string(got) != in {
			t.Errorf("toReasoningEffort(%q) = %q (err %v), want it passed through", in, got, err)
		}
	}
	if _, err := toReasoningEffort("extreme"); err == nil {
		t.Error("expected an error for an unknown effort")
	}
}

// The vendor family the consumer prices against — OPENAI whatever the baseURL
// points at, because what the number depends on is whose token accounting the
// API speaks, not which host answered.
func TestProviderNamesTheVendorFamily(t *testing.T) {
	if got := (&Connector{}).Provider(); got != "OPENAI" {
		t.Errorf("Provider() = %q, want OPENAI", got)
	}
	var _ core.LLMProvider = (*Connector)(nil)
}
