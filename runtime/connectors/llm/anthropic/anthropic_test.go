package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
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

func TestTranslateUsage(t *testing.T) {
	if got := translateUsage(sdk.Usage{}); got != nil {
		t.Errorf("empty usage = %+v, want nil", got)
	}
	// InputTokens reports only the uncached remainder, so a fully cached prompt
	// leaves it at zero while the response did carry usage. Reporting nil there
	// would drop the one count that was actually there.
	if got := translateUsage(sdk.Usage{CacheReadInputTokens: 30}); got == nil || got.CachedTokens != 30 {
		t.Errorf("cache-only usage = %+v, want it reported", got)
	}
	// The same argument for a cache *write*: the first turn of a cached
	// conversation is mostly cache creation, and its input count can be small or
	// zero. It is real usage — and the dearest kind, billed above the input rate.
	if got := translateUsage(sdk.Usage{CacheCreationInputTokens: 4096}); got == nil ||
		got.CacheWriteTokens != 4096 {
		t.Errorf("cache-write-only usage = %+v, want it reported", got)
	}
	got := translateUsage(sdk.Usage{
		InputTokens:              100,
		OutputTokens:             40,
		CacheReadInputTokens:     30,
		CacheCreationInputTokens: 4096,
		OutputTokensDetails:      sdk.OutputTokensDetails{ThinkingTokens: 25},
	})
	want := core.LLMUsage{
		InputTokens: 100, OutputTokens: 40, ThinkingTokens: 25,
		CachedTokens: 30, CacheWriteTokens: 4096,
		// The whole prompt the model read: the uncached remainder plus both cache
		// halves. This is the one connector where that is a sum, which is the whole
		// reason PromptTokens exists as a field rather than as caller arithmetic.
		PromptTokens: 4226,
	}
	if got == nil || *got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
	// A prompt served entirely from cache still had a size, and InputTokens alone
	// would report it as nothing.
	if got := translateUsage(sdk.Usage{CacheReadInputTokens: 30}); got == nil || got.PromptTokens != 30 {
		t.Errorf("fully cached prompt = %+v, want promptTokens 30", got)
	}
}

// TestThinkingRoundTrip is the correctness case for extended thinking: the server
// validates the thinking runs of an echoed assistant turn, so a reasoning block
// captured from one response has to go back out on the next request unchanged and
// in its original position. It also pins that reasoning stays out of Text, which
// is what stops a caller folding the answer into a body from publishing it.
func TestThinkingRoundTrip(t *testing.T) {
	const cannedResponse = `{
		"id": "msg_1",
		"type": "message",
		"role": "assistant",
		"model": "claude-sonnet-4-6",
		"content": [
			{"type": "thinking", "thinking": "the user wants billing", "signature": "sig-abc"},
			{"type": "redacted_thinking", "data": "encrypted-blob"},
			{"type": "text", "text": "routing"},
			{"type": "tool_use", "id": "tu_1", "name": "select_route", "input": {"route": "billing"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 5, "output_tokens": 30, "output_tokens_details": {"thinking_tokens": 20}}
	}`

	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = nil
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
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "billing question"}},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if resp.Text != "routing" {
		t.Errorf("text = %q, want %q — reasoning must not reach Text", resp.Text, "routing")
	}
	if len(resp.Raw.Thinking) != 2 {
		t.Fatalf("thinking blocks = %d, want 2: %+v", len(resp.Raw.Thinking), resp.Raw.Thinking)
	}
	if got := resp.Raw.Thinking[0]; got.Text != "the user wants billing" || got.Signature != "sig-abc" {
		t.Errorf("thinking[0] = %+v, want the text and its signature", got)
	}
	if got := string(resp.Raw.Thinking[1].Redacted); got != "encrypted-blob" {
		t.Errorf("thinking[1] redacted = %q, want the opaque blob", got)
	}
	if resp.Usage == nil || resp.Usage.ThinkingTokens != 20 {
		t.Errorf("usage = %+v, want 20 thinking tokens", resp.Usage)
	}

	// Feed the assistant turn back, as a tool loop does, and read what went on the
	// wire. Both blocks must lead the turn, in order, ahead of text and tool_use.
	if _, err := c.Complete(context.Background(), core.LLMRequest{
		Messages: []core.LLMMessage{
			{Role: core.LLMRoleUser, Text: "billing question"},
			resp.Raw,
		},
	}); err != nil {
		t.Fatalf("second complete: %v", err)
	}

	blocks := assistantContent(t, gotBody)
	if len(blocks) != 4 {
		t.Fatalf("echoed content blocks = %d, want 4: %+v", len(blocks), blocks)
	}
	if blocks[0]["type"] != "thinking" || blocks[0]["signature"] != "sig-abc" ||
		blocks[0]["thinking"] != "the user wants billing" {
		t.Errorf("block 0 = %+v, want the thinking block verbatim and first", blocks[0])
	}
	if blocks[1]["type"] != "redacted_thinking" || blocks[1]["data"] != "encrypted-blob" {
		t.Errorf("block 1 = %+v, want the redacted block verbatim", blocks[1])
	}
	if blocks[2]["type"] != "text" || blocks[3]["type"] != "tool_use" {
		t.Errorf("blocks 2,3 = %+v, %+v, want text then tool_use after the thinking run", blocks[2], blocks[3])
	}
}

// assistantContent digs the content blocks of the last message out of a captured
// request body.
func assistantContent(t *testing.T, body map[string]any) []map[string]any {
	t.Helper()
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) == 0 {
		t.Fatalf("request had no messages: %+v", body)
	}
	last, ok := msgs[len(msgs)-1].(map[string]any)
	if !ok {
		t.Fatalf("last message not an object: %+v", msgs[len(msgs)-1])
	}
	raw, ok := last["content"].([]any)
	if !ok {
		t.Fatalf("last message had no content array: %+v", last)
	}
	out := make([]map[string]any, 0, len(raw))
	for _, b := range raw {
		block, blockOK := b.(map[string]any)
		if !blockOK {
			t.Fatalf("content block not an object: %+v", b)
		}
		out = append(out, block)
	}
	return out
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

func TestToThinking(t *testing.T) {
	const maxTokens = 4096

	off, err := toThinking(connectorSettings{}, maxTokens)
	if err != nil || off.OfEnabled != nil || off.OfAdaptive != nil {
		t.Errorf("unset thinking = %+v (err %v), want nothing sent", off, err)
	}

	adaptive, err := toThinking(connectorSettings{Thinking: thinkingAdaptive}, maxTokens)
	if err != nil || adaptive.OfAdaptive == nil {
		t.Errorf("adaptive = %+v (err %v), want the adaptive variant", adaptive, err)
	}
	if adaptive.OfEnabled != nil {
		t.Error("adaptive must not use the enabled variant: the SDK deprecates it and warns on stderr")
	}

	budgeted, err := toThinking(connectorSettings{Thinking: thinkingBudgeted, ThinkingBudget: 2048}, maxTokens)
	if err != nil || budgeted.OfEnabled == nil || budgeted.OfEnabled.BudgetTokens != 2048 {
		t.Errorf("budgeted = %+v (err %v), want an enabled config of 2048", budgeted, err)
	}

	// Both bounds are ours to enforce; the SDK checks neither and the API answers 400.
	if _, err := toThinking(connectorSettings{Thinking: thinkingBudgeted, ThinkingBudget: 512}, maxTokens); err == nil {
		t.Error("expected an error for a budget below the provider floor")
	}
	if _, err := toThinking(connectorSettings{Thinking: thinkingBudgeted, ThinkingBudget: 8192}, maxTokens); err == nil {
		t.Error("expected an error for a budget at or above maxTokens")
	}
	if _, err := toThinking(connectorSettings{Thinking: "sometimes"}, maxTokens); err == nil {
		t.Error("expected an error for an unknown thinking mode")
	}
}

// TestThinkingYieldsToForcedToolChoice pins the reconciliation: extended thinking
// and a forced tool choice are a 400 at the API and the SDK checks neither, so one
// has to give. Thinking does, which is what lets a thinking-enabled connector
// still serve an ai-router.
func TestThinkingYieldsToForcedToolChoice(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = nil
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"m",
			"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "claude",
		Settings: types.Settings{
			"apiKey": "sk-test", "baseURL": srv.URL, "thinking": thinkingAdaptive,
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	tools := []core.LLMTool{{Name: "pick", Description: "pick", InputSchema: json.RawMessage(`{"type":"object"}`)}}
	base := core.LLMRequest{
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}},
		Tools:    tools,
	}

	for _, tc := range []struct {
		name   string
		choice core.LLMToolChoice
		want   bool
	}{
		{"auto keeps thinking", core.LLMToolChoice{}, true},
		{"none keeps thinking", core.LLMToolChoice{Mode: core.LLMToolChoiceNone}, true},
		{"any drops thinking", core.LLMToolChoice{Mode: core.LLMToolChoiceAny}, false},
		{"named tool drops thinking", core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "pick"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.ToolChoice = tc.choice
			if _, err := c.Complete(context.Background(), req); err != nil {
				t.Fatalf("complete: %v", err)
			}
			_, sent := gotBody["thinking"]
			if sent != tc.want {
				t.Errorf("thinking sent = %v, want %v (request body: %+v)", sent, tc.want, gotBody)
			}
		})
	}
}

// TestThinkingYieldsToASmallerRequestMaxTokens covers the half of the budget bound
// that startup validation structurally cannot: toThinking checks the budget against
// the connector's own maxTokens, but any AI block may override maxTokens per call,
// so a connector that started cleanly can still put budget >= max_tokens on the wire
// and take a 400. The budget has to be re-checked against the request that is
// actually being sent.
func TestThinkingYieldsToASmallerRequestMaxTokens(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody = nil
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"m","type":"message","role":"assistant","model":"m",
			"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()

	// Valid at startup: the budget sits well below the connector's own maxTokens.
	budgeted := &Connector{}
	if err := budgeted.Start(context.Background(), types.ConnectorConfig{
		Name: "claude",
		Settings: types.Settings{
			"apiKey": "sk-test", "baseURL": srv.URL,
			"thinking": thinkingBudgeted, "thinkingBudget": 2048, "maxTokens": 8192,
		},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}

	base := core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}}

	for _, tc := range []struct {
		name      string
		maxTokens int
		want      bool
	}{
		{"connector default leaves room", 0, true},
		{"a larger override leaves room", 4096, true},
		{"an override equal to the budget does not", 2048, false},
		{"an override below the budget does not", 1024, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.MaxTokens = tc.maxTokens
			if _, err := budgeted.Complete(context.Background(), req); err != nil {
				t.Fatalf("complete: %v", err)
			}
			if _, sent := gotBody["thinking"]; sent != tc.want {
				t.Errorf("thinking sent = %v, want %v (request body: %+v)", sent, tc.want, gotBody)
			}
		})
	}

	// Adaptive has no budget of its own — the model decides within whatever
	// max_tokens the request carries — so no override can put it out of bounds.
	adaptive := &Connector{}
	if err := adaptive.Start(context.Background(), types.ConnectorConfig{
		Name: "claude",
		Settings: types.Settings{
			"apiKey": "sk-test", "baseURL": srv.URL, "thinking": thinkingAdaptive,
		},
	}); err != nil {
		t.Fatalf("start adaptive: %v", err)
	}
	t.Run("adaptive survives any override", func(t *testing.T) {
		req := base
		req.MaxTokens = 16
		if _, err := adaptive.Complete(context.Background(), req); err != nil {
			t.Fatalf("complete: %v", err)
		}
		if _, sent := gotBody["thinking"]; !sent {
			t.Errorf("adaptive thinking dropped on a small maxTokens (request body: %+v)", gotBody)
		}
	})
}

// The vendor family the consumer prices against. Asserted as a literal rather
// than against the constant, so a change to either is a change to a wire
// contract and shows up as a failing test rather than as a silent re-spelling.
func TestProviderNamesTheVendorFamily(t *testing.T) {
	if got := (&Connector{}).Provider(); got != "ANTHROPIC" {
		t.Errorf("Provider() = %q, want ANTHROPIC", got)
	}
	var _ core.LLMProvider = (*Connector)(nil)
}
