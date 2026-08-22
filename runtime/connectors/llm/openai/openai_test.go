package openai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/responses"
	"github.com/openai/openai-go/v2/shared"

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

func TestMapStopReason(t *testing.T) {
	cases := []struct {
		name     string
		reason   string
		hasCalls bool
		refused  bool
		want     core.LLMStopReason
	}{
		// Responses names no finish reason for an ordinary turn: a turn that wants
		// tools just ends completed with function_call items in its output.
		{name: "calls mean tool use", hasCalls: true, want: core.LLMStopToolUse},
		{name: "truncated", reason: "max_output_tokens", want: core.LLMStopMaxTokens},
		{name: "filtered", reason: "content_filter", want: core.LLMStopRefusal},
		{name: "plain end", want: core.LLMStopEndTurn},
		{name: "refusal outranks calls", hasCalls: true, refused: true, want: core.LLMStopRefusal},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			resp := &responses.Response{IncompleteDetails: responses.ResponseIncompleteDetails{Reason: tt.reason}}
			if got := mapStopReason(resp, tt.hasCalls, tt.refused); got != tt.want {
				t.Errorf("mapStopReason = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestToToolChoice(t *testing.T) {
	if _, ok := toToolChoice(core.LLMToolChoice{}); ok {
		t.Error("auto (zero) mode should signal unset")
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceAny}); !ok ||
		c.OfToolChoiceMode.Value != responses.ToolChoiceOptionsRequired {
		t.Errorf("any mode should map to required: %+v", c)
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceNone}); !ok ||
		c.OfToolChoiceMode.Value != responses.ToolChoiceOptionsNone {
		t.Errorf("none mode should map to none: %+v", c)
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "pick"}); !ok ||
		c.OfFunctionTool == nil || c.OfFunctionTool.Name != "pick" {
		t.Errorf("tool mode should set the named function tool choice: %+v", c)
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
	fn := tools[0].OfFunction
	if fn.Name != "select_route" || fn.Description.Value != "pick a route" || fn.Parameters["type"] != "object" {
		t.Errorf("function definition not mapped: %+v", fn)
	}
	// Strict defaults to on, and on demands a schema shape a hand-written
	// inputSchema will not have — every property required, additionalProperties
	// false. The connector passes the author's schema through, so strict is off.
	if fn.Strict.Value {
		t.Error("strict must be off: the connector passes an author's schema through verbatim")
	}
}

func TestToInputRoles(t *testing.T) {
	input, err := toInput([]core.LLMMessage{
		{Role: core.LLMRoleUser, Text: "hi"},
		{Role: core.LLMRoleAssistant, Text: "ok", ToolCalls: []core.LLMToolCall{
			{ID: "t1", Name: "look", Input: json.RawMessage(`{"q":"x"}`)},
		}},
		{Role: core.LLMRoleTool, ToolResults: []core.LLMToolResult{
			{ToolCallID: "t1", Tool: "look", Content: `{"ok":true}`},
		}},
	})
	if err != nil {
		t.Fatalf("toInput: %v", err)
	}
	// user + assistant text + assistant call + call output = 4. The system prompt is
	// not an item here: Responses carries it as instructions.
	if len(input) != 4 {
		t.Fatalf("items = %d, want 4: %+v", len(input), input)
	}
	if input[1].OfMessage == nil {
		t.Errorf("item 1 should be the assistant's text: %+v", input[1])
	}
	if input[2].OfFunctionCall == nil || input[2].OfFunctionCall.CallID != "t1" {
		t.Errorf("item 2 should be the function call: %+v", input[2])
	}
	if input[3].OfFunctionCallOutput == nil || input[3].OfFunctionCallOutput.CallID != "t1" {
		t.Errorf("item 3 should be the call output: %+v", input[3])
	}
	if _, err := toInput([]core.LLMMessage{{Role: "bogus"}}); err == nil {
		t.Error("expected error for unknown role")
	}
}

// TestReasoningIsEchoedBeforeItsCall pins the ordering the server enforces: a
// reasoning item must precede the function call it produced, or the turn is
// rejected as a call whose reasoning went missing. Nothing is stored on the
// provider's side, so the encrypted content is the only thing that carries the
// reasoning forward.
func TestReasoningIsEchoedBeforeItsCall(t *testing.T) {
	input, err := toInput([]core.LLMMessage{{
		Role: core.LLMRoleAssistant,
		Thinking: []core.LLMThinkingBlock{
			{Text: "weighing it up", Signature: "rs_1", Redacted: []byte("gAAAA")},
		},
		ToolCalls: []core.LLMToolCall{{ID: "call_1", Name: "look"}},
	}})
	if err != nil {
		t.Fatalf("toInput: %v", err)
	}
	if len(input) != 2 {
		t.Fatalf("items = %d, want 2 (reasoning then call)", len(input))
	}
	reasoning := input[0].OfReasoning
	if reasoning == nil {
		t.Fatalf("item 0 should be the reasoning: %+v", input[0])
	}
	if reasoning.ID != "rs_1" || reasoning.EncryptedContent.Value != "gAAAA" {
		t.Errorf("reasoning item lost its id or encrypted content: %+v", reasoning)
	}
	if input[1].OfFunctionCall == nil {
		t.Errorf("item 1 should be the function call: %+v", input[1])
	}
}

// TestEchoedReasoningAlwaysCarriesASummary pins the field on the wire, not on the
// struct, because that is where it went missing.
//
// A reasoning item's summary is required even when empty, and empty is the
// ordinary case: a turn that never asked for a summary still returns reasoning
// items carrying encrypted content and nothing else. A nil slice is dropped by the
// encoder rather than sent as [], so the server saw no field at all and refused
// the request — "Missing required parameter: 'input[1].summary'" — which killed
// every turn after a tool call. Asserting on the struct would not have caught it;
// the struct was fine and the bytes were not.
func TestEchoedReasoningAlwaysCarriesASummary(t *testing.T) {
	for _, tt := range []struct {
		name string
		text string
	}{
		{name: "no summary returned", text: ""},
		{name: "summary returned", text: "weighing it up"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input, err := toInput([]core.LLMMessage{{
				Role:     core.LLMRoleAssistant,
				Thinking: []core.LLMThinkingBlock{{Text: tt.text, Signature: "rs_1", Redacted: []byte("gAAAA")}},
			}})
			if err != nil {
				t.Fatalf("toInput: %v", err)
			}
			raw, err := json.Marshal(input[0].OfReasoning)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var onWire map[string]any
			if err := json.Unmarshal(raw, &onWire); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if _, ok := onWire["summary"]; !ok {
				t.Errorf("summary is absent from the request: %s", raw)
			}
		})
	}
}

// TestReasoningWithoutASignatureIsDropped pins that a block with no id is left out
// rather than sent. The id is what the server matches the echo against, so an item
// invented without one is not the model's reasoning coming back — it is a new item
// the server has never seen.
func TestReasoningWithoutASignatureIsDropped(t *testing.T) {
	input, err := toInput([]core.LLMMessage{{
		Role:     core.LLMRoleAssistant,
		Thinking: []core.LLMThinkingBlock{{Text: "no id here"}},
		Text:     "hello",
	}})
	if err != nil {
		t.Fatalf("toInput: %v", err)
	}
	if len(input) != 1 || input[0].OfMessage == nil {
		t.Errorf("items = %+v, want only the assistant text", input)
	}
}

// TestCompleteEndToEnd drives Complete against a canned OpenAI response served by
// an httptest server, proving request marshaling and response translation —
// including that reasoning is collected as reasoning rather than folded into the
// answer, which is what keeps a caller from publishing the model's own reasoning.
func TestCompleteEndToEnd(t *testing.T) {
	const cannedResponse = `{
		"id": "resp_1",
		"object": "response",
		"created_at": 1,
		"model": "gpt-5.6-luna",
		"status": "completed",
		"output": [
			{"type": "reasoning", "id": "rs_1", "encrypted_content": "gAAAA",
			 "summary": [{"type": "summary_text", "text": "billing, then"}]},
			{"type": "message", "id": "msg_1", "role": "assistant", "status": "completed",
			 "content": [{"type": "output_text", "text": "routing", "annotations": []}]},
			{"type": "function_call", "id": "fc_1", "call_id": "call_1",
			 "name": "select_route", "arguments": "{\"route\":\"billing\"}"}
		],
		"usage": {"input_tokens": 5, "output_tokens": 3, "total_tokens": 8,
		          "input_tokens_details": {"cached_tokens": 0},
		          "output_tokens_details": {"reasoning_tokens": 2}}
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

	// Reasoning is carried on the turn, never in the answer.
	if strings.Contains(resp.Text, "billing, then") {
		t.Error("the reasoning summary leaked into the answer text")
	}
	if len(resp.Raw.Thinking) != 1 {
		t.Fatalf("thinking blocks = %d, want 1: %+v", len(resp.Raw.Thinking), resp.Raw.Thinking)
	}
	tb := resp.Raw.Thinking[0]
	if tb.Text != "billing, then" || tb.Signature != "rs_1" || string(tb.Redacted) != "gAAAA" {
		t.Errorf("thinking block = %+v", tb)
	}
	if resp.Usage == nil || resp.Usage.ThinkingTokens != 2 {
		t.Errorf("usage = %+v, want 2 thinking tokens", resp.Usage)
	}

	// The system prompt rides as instructions rather than as an input item, and the
	// tool choice names the function.
	if gotBody["instructions"] != "you route tickets" {
		t.Errorf("request instructions = %v", gotBody["instructions"])
	}
	if items, ok := gotBody["input"].([]any); !ok || len(items) == 0 {
		t.Errorf("request input missing: %v", gotBody["input"])
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
	if got := translateUsage(responses.ResponseUsage{}); got != nil {
		t.Errorf("empty usage = %+v, want nil", got)
	}
	got := translateUsage(responses.ResponseUsage{
		InputTokens:         100,
		OutputTokens:        40,
		OutputTokensDetails: responses.ResponseUsageOutputTokensDetails{ReasoningTokens: 25},
		InputTokensDetails:  responses.ResponseUsageInputTokensDetails{CachedTokens: 30},
	})
	// OutputTokens already counts reasoning, so it passes through — and InputTokens
	// already counts the cached reads, so PromptTokens equals it rather than summing.
	want := core.LLMUsage{
		InputTokens: 100, OutputTokens: 40, ThinkingTokens: 25, CachedTokens: 30,
		PromptTokens: 100,
	}
	if got == nil || *got != want {
		t.Errorf("usage = %+v, want %+v", got, want)
	}
}

func TestToReasoningEffort(t *testing.T) {
	// Unset means default, and default sends no reasoning field at all. That is the
	// only setting safe on every model this connector can be pointed at: reasoning is
	// documented for gpt-5 and o-series only, and baseURL aims this at Azure, proxies
	// and other compatible servers whose model list is not OpenAI's.
	for _, in := range []string{"", "default"} {
		if got, err := toReasoningEffort(in); err != nil || got != "" {
			t.Errorf("toReasoningEffort(%q) = %q (err %v), want the field omitted", in, got, err)
		}
	}
	for _, in := range []string{"none", "minimal", "low", "medium", "high"} {
		if got, err := toReasoningEffort(in); err != nil || string(got) != in {
			t.Errorf("toReasoningEffort(%q) = %q (err %v), want it passed through", in, got, err)
		}
	}
	// "off" was the original spelling and is gone rather than aliased, per the
	// standard that prefers a complete refactor to a compatibility shim.
	if _, err := toReasoningEffort("off"); err == nil {
		t.Error("expected an error for the removed \"off\" spelling")
	}
	if _, err := toReasoningEffort("extreme"); err == nil {
		t.Error("expected an error for an unknown effort")
	}
}

// TestDefaultReasoningSendsNoReasoningField pins the request shape the default
// produces, because it is the setting that broke and the one every install gets:
// an explicit "none" is what stopped a reasoning model reasoning, and a model told
// not to reason answered in the shape of its input instead of the shape its prompt
// asked for.
func TestDefaultReasoningSendsNoReasoningField(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "gpt", Settings: types.Settings{"apiKey": "sk-test"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	params, err := c.params(core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if !param.IsOmitted(params.Reasoning) {
		t.Errorf("reasoning = %+v, want the field omitted by default", params.Reasoning)
	}
	if len(params.Include) != 0 {
		t.Errorf("include = %+v, want nothing asked for when reasoning is not configured", params.Include)
	}
}

// TestConfiguredReasoningAsksForASummaryAndItsEncryptedForm pins the two things a
// reasoning turn needs beyond the effort itself: a summary, which is the only
// readable reasoning Responses returns, and the encrypted content, which is what
// carries reasoning to the next turn when nothing is stored server-side.
func TestConfiguredReasoningAsksForASummaryAndItsEncryptedForm(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "gpt", Settings: types.Settings{"apiKey": "sk-test", "reasoning": "medium"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	params, err := c.params(core.LLMRequest{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if params.Reasoning.Effort != "medium" || params.Reasoning.Summary != shared.ReasoningSummaryAuto {
		t.Errorf("reasoning = %+v, want medium effort with an auto summary", params.Reasoning)
	}
	if len(params.Include) != 1 || params.Include[0] != responses.ResponseIncludableReasoningEncryptedContent {
		t.Errorf("include = %+v, want the encrypted reasoning content", params.Include)
	}
	if params.Store.Value {
		t.Error("store must be off: the conversation is sent whole every turn")
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
