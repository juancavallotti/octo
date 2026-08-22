package openrouter

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
	err := c.Start(context.Background(), types.ConnectorConfig{Name: "router", Settings: types.Settings{}})
	if err == nil || !strings.Contains(err.Error(), "apiKey is required") {
		t.Fatalf("expected apiKey-required error, got %v", err)
	}
}

func TestStartAppliesDefaults(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "router", Settings: types.Settings{"apiKey": "sk-or-test"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if c.model != defaultModel {
		t.Errorf("model = %q, want default %q", c.model, defaultModel)
	}
	// The default is silence, not an effort: OpenRouter routes to models with no
	// reasoning to configure, and a reasoning field they do not understand fails
	// every call.
	if c.reasoning != "" {
		t.Errorf("reasoning = %q, want it unset by default", c.reasoning)
	}
}

func TestStartRejectsAnUnknownReasoningEffort(t *testing.T) {
	c := &Connector{}
	err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "router", Settings: types.Settings{"apiKey": "sk-or-test", "reasoning": "enormous"},
	})
	if err == nil || !strings.Contains(err.Error(), "reasoning must be one of") {
		t.Fatalf("expected a reasoning-enum error, got %v", err)
	}
}

// The vendor family is OPENROUTER, not the vendor behind whichever model
// answered. It decides how cached tokens are charged downstream, and these counts
// are OpenRouter's.
func TestProviderNamesTheVendorFamily(t *testing.T) {
	if got := (&Connector{}).Provider(); got != core.ProviderOpenRouter {
		t.Errorf("Provider() = %q, want %q", got, core.ProviderOpenRouter)
	}
	if core.ProviderOpenRouter == core.ProviderOpenAI {
		t.Error("OpenRouter must not be reported as OpenAI: the model ids and the rate card differ")
	}
}

func TestToReasoningEffort(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: ""},
		{in: "default", want: ""},
		{in: "none", want: "none"},
		{in: "minimal", want: "minimal"},
		{in: "low", want: "low"},
		{in: "medium", want: "medium"},
		{in: "high", want: "high"},
		{in: "maximum", wantErr: true},
	}
	for _, tt := range cases {
		t.Run(tt.in, func(t *testing.T) {
			got, err := toReasoningEffort(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("toReasoningEffort(%q) = %q, want an error", tt.in, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("toReasoningEffort(%q) = %q, %v, want %q", tt.in, got, err, tt.want)
			}
		})
	}
}

func TestReasoningField(t *testing.T) {
	if _, ok := reasoningField(""); ok {
		t.Error("the default should send no reasoning field at all")
	}
	// Switched off explicitly is a different instruction from saying nothing: a
	// model that reasons by default stops.
	got, ok := reasoningField("none")
	if !ok || got["enabled"] != false {
		t.Errorf("none = %+v, want {enabled:false}", got)
	}
	got, ok = reasoningField("high")
	if !ok || got["effort"] != "high" {
		t.Errorf("high = %+v, want {effort:high}", got)
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
		{name: "named tool calls", reason: finishToolCalls, want: core.LLMStopToolUse},
		// Upstreams do not all spell the reason the same way, so the calls
		// themselves are what settle it.
		{name: "calls without the reason", reason: "stop", hasCalls: true, want: core.LLMStopToolUse},
		{name: "truncated", reason: finishLength, want: core.LLMStopMaxTokens},
		{name: "filtered", reason: finishContentFilter, want: core.LLMStopRefusal},
		{name: "plain end", reason: "stop", want: core.LLMStopEndTurn},
		{name: "unknown reason ends the turn", reason: "who_knows", want: core.LLMStopEndTurn},
		{name: "refusal outranks calls", hasCalls: true, refused: true, want: core.LLMStopRefusal},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapStopReason(tt.reason, tt.hasCalls, tt.refused); got != tt.want {
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
		c.OfAuto.Value != "required" {
		t.Errorf("any mode should map to required: %+v", c)
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceNone}); !ok ||
		c.OfAuto.Value != "none" {
		t.Errorf("none mode should map to none: %+v", c)
	}
	if c, ok := toToolChoice(core.LLMToolChoice{Mode: core.LLMToolChoiceTool, Name: "pick"}); !ok ||
		c.OfFunctionToolChoice == nil || c.OfFunctionToolChoice.Function.Name != "pick" {
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
		t.Fatalf("tools = %+v, want one function tool", tools)
	}
	fn := tools[0].OfFunction.Function
	if fn.Name != "select_route" || fn.Description.Value != "pick a route" {
		t.Errorf("function = %+v", fn)
	}
	// Strict off, so an author-written schema is passed through rather than
	// rejected for a shape they never agreed to.
	if fn.Strict.Value {
		t.Error("strict should be off")
	}
	if _, ok := fn.Parameters["properties"]; !ok {
		t.Errorf("parameters = %+v, want the decoded schema", fn.Parameters)
	}
}

func TestToToolsRejectsABadSchema(t *testing.T) {
	if _, err := toTools([]core.LLMTool{{Name: "t", InputSchema: json.RawMessage(`{oops`)}}); err == nil {
		t.Fatal("expected an error for a schema that is not JSON")
	}
}

func TestToMessagesRoles(t *testing.T) {
	msgs, err := toMessages("you route tickets", []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: "hello"},
		{Role: core.LLMRoleAssistant, Text: "thinking", ToolCalls: []core.LLMToolCall{
			{ID: "tc_1", Name: "look", Input: json.RawMessage(`{"q":"refund"}`)},
		}},
		{Role: core.LLMRoleTool, ToolResults: []core.LLMToolResult{
			{ToolCallID: "tc_1", Content: "nothing found"},
		}},
	})
	if err != nil {
		t.Fatalf("toMessages: %v", err)
	}
	// The system prompt leads as a message of its own: Chat Completions has no
	// field for it.
	if len(msgs) != 4 || msgs[0].OfSystem == nil {
		t.Fatalf("messages = %d, want 4 led by the system message: %+v", len(msgs), msgs)
	}
	if msgs[1].OfUser == nil || msgs[2].OfAssistant == nil || msgs[3].OfTool == nil {
		t.Errorf("roles did not map: %+v", msgs)
	}
	if calls := msgs[2].OfAssistant.ToolCalls; len(calls) != 1 || calls[0].OfFunction.ID != "tc_1" {
		t.Errorf("assistant tool calls = %+v", calls)
	}
	if msgs[3].OfTool.ToolCallID != "tc_1" {
		t.Errorf("tool result lost its call id: %+v", msgs[3].OfTool)
	}
}

func TestToMessagesRejectsAnUnknownRole(t *testing.T) {
	_, err := toMessages("", []core.LLMMessage{{Role: "oracle", Text: "hi"}})
	if err == nil || !strings.Contains(err.Error(), "unknown message role") {
		t.Fatalf("expected an unknown-role error, got %v", err)
	}
}

// A failed tool result is marked in the text, because Chat Completions has no
// is-error flag on a tool message and a model that cannot tell a failure from a
// result will treat the failure as an answer.
func TestToMessagesMarksAFailedToolResult(t *testing.T) {
	msgs, err := toMessages("", []core.LLMMessage{{
		Role:        core.LLMRoleTool,
		ToolResults: []core.LLMToolResult{{ToolCallID: "tc_1", Content: "boom", IsError: true}},
	}})
	if err != nil {
		t.Fatalf("toMessages: %v", err)
	}
	if got := msgs[0].OfTool.Content.OfString.Value; !strings.HasPrefix(got, "ERROR: ") {
		t.Errorf("content = %q, want it marked as an error", got)
	}
}

// Reasoning goes back as the opaque block it arrived as. A block rebuilt from the
// readable summary would carry no signature the upstream recognises, which is
// worse than sending nothing.
func TestAssistantEchoesReasoningDetailsVerbatim(t *testing.T) {
	details := json.RawMessage(`[{"type":"reasoning.text","text":"hm","signature":"sig-1"}]`)
	msg := assistantMessage(core.LLMMessage{
		Role:     core.LLMRoleAssistant,
		Text:     "answer",
		Thinking: []core.LLMThinkingBlock{{Text: "hm", Redacted: details}},
	})

	body, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	echoed, ok := got[fieldReasoningDetails].([]any)
	if !ok || len(echoed) != 1 {
		t.Fatalf("reasoning_details = %v, want the block echoed verbatim", got[fieldReasoningDetails])
	}
	if echoed[0].(map[string]any)["signature"] != "sig-1" {
		t.Errorf("echoed block lost its signature: %v", echoed[0])
	}
}

// A thinking block with nothing opaque to echo is dropped rather than sent as an
// empty one, on the same rule.
func TestAssistantWithoutReasoningDetailsSendsNone(t *testing.T) {
	msg := assistantMessage(core.LLMMessage{
		Role:     core.LLMRoleAssistant,
		Text:     "answer",
		Thinking: []core.LLMThinkingBlock{{Text: "hm"}},
	})
	body, _ := json.Marshal(msg)
	if strings.Contains(string(body), fieldReasoningDetails) {
		t.Errorf("body = %s, want no reasoning_details", body)
	}
}

func TestTranslateUsage(t *testing.T) {
	var u sdk.CompletionUsage
	raw := `{"prompt_tokens":100,"completion_tokens":30,"total_tokens":130,"cost":0.0042,
		"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":5},
		"completion_tokens_details":{"reasoning_tokens":12}}`
	if err := json.Unmarshal([]byte(raw), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	got := translateUsage(u)
	if got == nil {
		t.Fatal("usage = nil, want it translated")
	}
	// Output already counts reasoning inside itself, and input already counts the
	// tokens served from cache. Both are the OpenAI-compatible convention.
	if got.InputTokens != 100 || got.OutputTokens != 30 || got.ThinkingTokens != 12 {
		t.Errorf("tokens = %+v", got)
	}
	if got.CachedTokens != 80 || got.CacheWriteTokens != 5 {
		t.Errorf("cache tokens = %+v", got)
	}
	if got.ReportedCostUSD == nil || *got.ReportedCostUSD != 0.0042 {
		t.Errorf("cost = %v, want 0.0042", got.ReportedCostUSD)
	}
}

func TestTranslateUsageEmptyIsNil(t *testing.T) {
	if got := translateUsage(sdk.CompletionUsage{}); got != nil {
		t.Errorf("usage = %+v, want nil: a provider reporting nothing is not a provider charging nothing", got)
	}
}

// A response that reports tokens but no cost leaves the cost unset rather than
// zero, so nothing downstream can read "not reported" as "free".
func TestTranslateUsageWithoutACostLeavesItNil(t *testing.T) {
	var u sdk.CompletionUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}`), &u); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	got := translateUsage(u)
	if got == nil || got.ReportedCostUSD != nil {
		t.Errorf("usage = %+v, want tokens with no cost", got)
	}
}

func TestServedBy(t *testing.T) {
	if got := servedBy("anthropic/claude-sonnet-4.5", "anthropic/claude-sonnet-4"); got != "anthropic/claude-sonnet-4.5" {
		t.Errorf("servedBy = %q, want what the provider echoed", got)
	}
	if got := servedBy("", "openai/gpt-5.4"); got != "openai/gpt-5.4" {
		t.Errorf("servedBy = %q, want the configured model when none was echoed", got)
	}
}

func TestTurnFromCompletionRejectsAChoicelessResponse(t *testing.T) {
	if _, err := turnFromCompletion(&sdk.ChatCompletion{}); err == nil {
		t.Fatal("expected an error for a response with no choices")
	}
	if _, err := turnFromCompletion(nil); err == nil {
		t.Fatal("expected an error for a nil response")
	}
}

func TestCompleteEndToEnd(t *testing.T) {
	const cannedResponse = `{
		"id":"gen_1","object":"chat.completion","created":1,"model":"anthropic/claude-sonnet-4.5",
		"choices":[{"index":0,"finish_reason":"tool_calls","message":{
			"role":"assistant","content":"routing","refusal":null,
			"reasoning":"billing, then",
			"reasoning_details":[{"type":"reasoning.text","text":"billing, then","signature":"sig-1"}],
			"tool_calls":[{"index":0,"id":"call_1","type":"function",
				"function":{"name":"select_route","arguments":"{\"route\":\"billing\"}"}}]}}],
		"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8,"cost":0.00001,
			"prompt_tokens_details":{"cached_tokens":0},
			"completion_tokens_details":{"reasoning_tokens":2}}
	}`

	var gotBody map[string]any
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, cannedResponse)
	}))
	defer srv.Close()

	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "router",
		Settings: types.Settings{
			"apiKey": "sk-or-test", "baseURL": srv.URL, "reasoning": "medium",
			"maxTokens": 1024, "appName": "Octo", "siteURL": "https://octo.example",
		},
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

	// What was asked for.
	if gotBody["model"] != defaultModel {
		t.Errorf("model = %v, want %q", gotBody["model"], defaultModel)
	}
	if gotBody["max_tokens"] != float64(1024) {
		t.Errorf("max_tokens = %v, want 1024", gotBody["max_tokens"])
	}
	// Usage accounting is what carries the cost OpenRouter charged; a cost nobody
	// asked for is a cost nothing downstream can report.
	usage, _ := gotBody["usage"].(map[string]any)
	if usage == nil || usage["include"] != true {
		t.Errorf("usage = %v, want {include:true}", gotBody["usage"])
	}
	reasoning, _ := gotBody["reasoning"].(map[string]any)
	if reasoning == nil || reasoning["effort"] != "medium" {
		t.Errorf("reasoning = %v, want {effort:medium}", gotBody["reasoning"])
	}
	// Not a streamed request, so stream_options must be absent: the API rejects it.
	if _, ok := gotBody["stream_options"]; ok {
		t.Error("stream_options should not be sent on a blocking request")
	}
	if got := gotHeaders.Get(headerTitle); got != "Octo" {
		t.Errorf("%s = %q, want the configured app name", headerTitle, got)
	}
	if got := gotHeaders.Get(headerReferer); got != "https://octo.example" {
		t.Errorf("%s = %q, want the configured site URL", headerReferer, got)
	}

	// What came back.
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
	// Reasoning rides on the assistant turn, not in the answer, so a flow that
	// folds the answer into a body cannot publish the model's train of thought.
	if strings.Contains(resp.Text, "billing, then") {
		t.Error("reasoning leaked into the answer")
	}
	if len(resp.Raw.Thinking) != 1 || resp.Raw.Thinking[0].Text != "billing, then" {
		t.Fatalf("thinking = %+v", resp.Raw.Thinking)
	}
	if resp.Usage == nil || resp.Usage.ReportedCostUSD == nil || *resp.Usage.ReportedCostUSD != 0.00001 {
		t.Errorf("usage = %+v, want the reported cost carried", resp.Usage)
	}
	if resp.Model != "anthropic/claude-sonnet-4.5" {
		t.Errorf("model = %q, want what answered", resp.Model)
	}
}

// A per-request cap overrides the connector's default, which is the one thing a
// block may say about the model.
func TestRequestMaxTokensOverridesTheConnectorDefault(t *testing.T) {
	c := &Connector{model: defaultModel, maxTokens: 100}
	params, err := c.params(core.LLMRequest{MaxTokens: 7}, false)
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if params.MaxTokens.Value != 7 {
		t.Errorf("max_tokens = %d, want the request's 7", params.MaxTokens.Value)
	}
}

// stream_options is the only difference between the two paths, and it is there
// because the API rejects it on a request that is not streaming.
func TestStreamingParamsAskForUsage(t *testing.T) {
	c := &Connector{model: defaultModel}
	params, err := c.params(core.LLMRequest{}, true)
	if err != nil {
		t.Fatalf("params: %v", err)
	}
	if !params.StreamOptions.IncludeUsage.Value {
		t.Error("a streamed request should ask for usage, or the turn comes back with none")
	}
}
