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
	// Every result goes back as text under one key. It used to be spread when it
	// parsed as an object, which handed Gemini the tool's own keys and values to
	// interpret — see the $ref case below.
	if m := responseMap(core.LLMToolResult{Content: `{"a":1}`}); m["result"] != `{"a":1}` {
		t.Errorf("object content should go back as text under result: %#v", m)
	}
	if m := responseMap(core.LLMToolResult{Content: `[1,2]`}); m["result"] != `[1,2]` {
		t.Errorf("list content should go back as text under result: %#v", m)
	}
	if m := responseMap(core.LLMToolResult{Content: "boom", IsError: true}); m["error"] != "boom" {
		t.Errorf("error result should be reported under error: %#v", m)
	}
}

// The failure this exists to prevent: a tool returning OpenAPI or JSON Schema.
// Spread into the function response, every "#/components/schemas/..." was read as
// a reference to a part that did not exist, and the whole turn came back 400.
// Under one key there is nothing for the API to resolve.
func TestResponseMapDoesNotExposeRefsAsFields(t *testing.T) {
	doc := `{"components":{"schemas":{"x":{"$ref":"#/components/schemas/email.sendRequestBody"}}}}`

	m := responseMap(core.LLMToolResult{Content: doc})

	if len(m) != 1 || m["result"] != doc {
		t.Fatalf("the document should be one string under result, got %#v", m)
	}
	if _, spread := m["components"]; spread {
		t.Error("the document's own keys must not become function response fields")
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

// TestParallelCallsToOneToolGetDistinctIDs pins the fix for the collision that
// synthesizing an id from the function name alone caused.
//
// Gemini calls tools in parallel, and calling the same tool twice in one turn is
// ordinary rather than a corner: two reads of different paths through one
// `octo_api` tool is exactly what an agent does. When both calls carried the id
// `octo_api`, the two became one to everything correlating on it — the agent
// reported both under a single id and the panel drew one chip for two calls.
func TestParallelCallsToOneToolGetDistinctIDs(t *testing.T) {
	resp := translateResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "octo_api", Args: map[string]any{"path": "/a"}}},
				{FunctionCall: &genai.FunctionCall{Name: "octo_api", Args: map[string]any{"path": "/b"}}},
			}},
			FinishReason: genai.FinishReasonStop,
		}},
	}, "gemini-3.5-flash")

	if len(resp.ToolCalls) != 2 {
		t.Fatalf("tool calls = %d, want 2", len(resp.ToolCalls))
	}
	first, second := resp.ToolCalls[0], resp.ToolCalls[1]
	if first.ID == second.ID {
		t.Fatalf("both calls share the id %q; parallel calls to one tool must be distinguishable", first.ID)
	}
	if first.Name != "octo_api" || second.Name != "octo_api" {
		t.Errorf("names = %q/%q, want both octo_api", first.Name, second.Name)
	}

	// The results still have to reach Gemini addressed by *name*, which is what it
	// matches on — the id only tells the two calls apart.
	contents, err := toContents([]core.LLMMessage{{
		Role: core.LLMRoleTool,
		ToolResults: []core.LLMToolResult{
			{ToolCallID: first.ID, Tool: first.Name, Content: `{"ok":1}`},
			{ToolCallID: second.ID, Tool: second.Name, Content: `{"ok":2}`},
		},
	}})
	if err != nil {
		t.Fatalf("toContents: %v", err)
	}
	for i, part := range contents[0].Parts {
		fr := part.FunctionResponse
		if fr == nil || fr.Name != "octo_api" {
			t.Fatalf("response %d = %+v, want one named octo_api", i, fr)
		}
	}
	if a, b := contents[0].Parts[0].FunctionResponse, contents[0].Parts[1].FunctionResponse; a.ID == b.ID {
		t.Errorf("function responses share the id %q", a.ID)
	}
}

// TestProviderCallIDIsPreferred pins that an id Gemini itself supplies wins over
// the synthesized one — the synthesized id exists only because the field is
// usually empty, and echoing back an id the provider did not issue is how a
// multi-turn tool loop stops matching.
func TestProviderCallIDIsPreferred(t *testing.T) {
	resp := translateResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{ID: "call_abc", Name: "octo_api"}},
			}},
			FinishReason: genai.FinishReasonStop,
		}},
	}, "gemini-3.5-flash")

	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "call_abc" {
		t.Errorf("tool calls = %+v, want the provider's own id", resp.ToolCalls)
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
	if call.Name != "select_route" || call.ID != "select_route-0" ||
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

// TestEmbedEndToEnd drives Embed against a canned embeddings response, proving
// the request carries every input and the requested output dimensionality, and
// that the response vectors are returned in request order.
func TestEmbedEndToEnd(t *testing.T) {
	const cannedResponse = `{
		"embeddings": [
			{"values": [0.1, 0.2]},
			{"values": [0.3, 0.4]}
		]
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

	resp, err := c.Embed(context.Background(), core.EmbedRequest{
		Model:      "gemini-embedding-001",
		Input:      []string{"first", "second"},
		Dimensions: 2,
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if len(resp.Vectors) != 2 || resp.Vectors[0][0] != float32(0.1) || resp.Vectors[1][0] != float32(0.3) {
		t.Errorf("vectors = %+v", resp.Vectors)
	}

	// Gemini's embeddings API reports no token count, so nil usage is the honest
	// answer rather than a zero that would read as "the provider charged nothing".
	if resp.Usage != nil {
		t.Errorf("usage = %+v, want nil — Gemini reports no embedding tokens", resp.Usage)
	}
	// The model still has to be named: it is what a cost reader prices against, and
	// with nothing echoed the requested id is what was billed.
	if resp.Model != "gemini-embedding-001" {
		t.Errorf("model = %q, want the requested id", resp.Model)
	}

	reqs, ok := gotBody["requests"].([]any)
	if !ok || len(reqs) != 2 {
		t.Fatalf("request requests = %v, want a 2-element batch", gotBody["requests"])
	}
	first, ok := reqs[0].(map[string]any)
	if !ok || first["outputDimensionality"] != float64(2) {
		t.Errorf("request outputDimensionality = %v, want 2", first["outputDimensionality"])
	}
}

// TestEmbedRequiresInput proves Embed rejects an empty batch before making a
// request.
func TestEmbedRequiresInput(t *testing.T) {
	c := &Connector{}
	if err := c.Start(context.Background(), types.ConnectorConfig{
		Name: "gem", Settings: types.Settings{"apiKey": "test"},
	}); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := c.Embed(context.Background(), core.EmbedRequest{Model: "gemini-embedding-001"}); err == nil {
		t.Error("expected error for empty input")
	}
}

// TestTranslateResponseKeepsThoughtsOutOfText pins the two things enabling
// thinking on Gemini would otherwise get wrong: reasoning must not be folded into
// the answer, and thought tokens must be added into OutputTokens, since Gemini is
// the one provider that reports them outside its candidate count.
func TestTranslateResponseKeepsThoughtsOutOfText(t *testing.T) {
	out := translateResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Parts: []*genai.Part{
				{Text: "the user is asking about a balance", Thought: true, ThoughtSignature: []byte("sig-1")},
				{Text: "your balance is 42"},
			}},
			FinishReason: genai.FinishReasonStop,
		}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:        10,
			CandidatesTokenCount:    5,
			ThoughtsTokenCount:      7,
			CachedContentTokenCount: 3,
		},
	}, "gemini-2.5-flash")

	if out.Text != "your balance is 42" {
		t.Errorf("text = %q, want the answer only — reasoning must not reach Text", out.Text)
	}
	if len(out.Raw.Thinking) != 1 {
		t.Fatalf("thinking blocks = %d, want 1: %+v", len(out.Raw.Thinking), out.Raw.Thinking)
	}
	if got := out.Raw.Thinking[0]; got.Text != "the user is asking about a balance" || got.Signature != "sig-1" {
		t.Errorf("thinking[0] = %+v, want the thought text and its signature", got)
	}
	want := core.LLMUsage{InputTokens: 10, OutputTokens: 12, ThinkingTokens: 7, CachedTokens: 3}
	if out.Usage == nil || *out.Usage != want {
		t.Errorf("usage = %+v, want %+v (thoughts added into OutputTokens)", out.Usage, want)
	}
}

// TestTranslateResponseCarriesThoughtSignatureToCall pins the pre-existing
// behaviour that a signature sitting on a thought part before a function call is
// the one replayed on that call — capturing thoughts must not consume it.
func TestTranslateResponseCarriesThoughtSignatureToCall(t *testing.T) {
	out := translateResponse(&genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Parts: []*genai.Part{
				{Text: "I should look this up", Thought: true, ThoughtSignature: []byte("sig-call")},
				{FunctionCall: &genai.FunctionCall{Name: "lookup", Args: map[string]any{"q": "x"}}},
			}},
			FinishReason: genai.FinishReasonStop,
		}},
	}, "gemini-2.5-flash")
	if len(out.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(out.ToolCalls))
	}
	if got := string(out.ToolCalls[0].Signature); got != "sig-call" {
		t.Errorf("call signature = %q, want the preceding thought's signature", got)
	}
	if out.Usage != nil {
		t.Errorf("usage = %+v, want nil when the response reported none", out.Usage)
	}
}

func TestToThinkingConfig(t *testing.T) {
	off, err := toThinkingConfig(connectorSettings{})
	if err != nil || off != nil {
		t.Errorf("unset thinking = %+v (err %v), want nil so the request carries none", off, err)
	}

	dyn, err := toThinkingConfig(connectorSettings{Thinking: thinkingDynamic})
	if err != nil || dyn == nil || dyn.ThinkingBudget == nil || *dyn.ThinkingBudget != dynamicThinkingBudget {
		t.Fatalf("dynamic = %+v (err %v), want the dynamic sentinel budget", dyn, err)
	}
	if !dyn.IncludeThoughts {
		t.Error("IncludeThoughts must be set, or the model reasons but returns no thought parts to observe")
	}

	budgeted, err := toThinkingConfig(connectorSettings{Thinking: thinkingBudgeted, ThinkingBudget: 2048})
	if err != nil || budgeted == nil || budgeted.ThinkingBudget == nil || *budgeted.ThinkingBudget != 2048 {
		t.Fatalf("budgeted = %+v (err %v), want a budget of 2048", budgeted, err)
	}
	// The same reason as above, and the worse case: a budget is spent either way,
	// so without this the tokens are billed and no thought part ever comes back.
	if !budgeted.IncludeThoughts {
		t.Error("IncludeThoughts must be set, or the budget is spent with nothing to observe")
	}

	if _, err := toThinkingConfig(connectorSettings{Thinking: thinkingBudgeted}); err == nil {
		t.Error("expected an error for a budgeted config with no budget")
	}
	if _, err := toThinkingConfig(connectorSettings{Thinking: "sometimes"}); err == nil {
		t.Error("expected an error for an unknown thinking mode")
	}
}

// TestTranslateUsageEmptyIsNil pins the cross-provider meaning of a nil Usage:
// "the provider did not account for this turn". Gemini attaches its metadata
// struct more eagerly than the others, so an all-zero block has to report the
// same nothing that a missing block does — otherwise the identical emptiness
// reads as accounting on this connector and as none on the other two.
func TestTranslateUsageEmptyIsNil(t *testing.T) {
	if got := translateUsage(nil); got != nil {
		t.Errorf("nil metadata = %+v, want nil", got)
	}
	if got := translateUsage(&genai.GenerateContentResponseUsageMetadata{}); got != nil {
		t.Errorf("all-zero metadata = %+v, want nil", got)
	}
	// Thinking tokens alone are still accounting, and they are billed.
	got := translateUsage(&genai.GenerateContentResponseUsageMetadata{ThoughtsTokenCount: 12})
	if got == nil || got.ThinkingTokens != 12 || got.OutputTokens != 12 {
		t.Errorf("thoughts-only metadata = %+v, want it reported with output inclusive", got)
	}
}

// The model that answered is not always the model that was asked for: a
// configured alias resolves to a dated snapshot, and it is the snapshot that was
// billed. Pairing token usage with the alias would attribute the cost to
// something that does not have a price.
func TestTranslateResponsePrefersTheServedModel(t *testing.T) {
	answer := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content:      &genai.Content{Parts: []*genai.Part{{Text: "ok"}}},
			FinishReason: genai.FinishReasonStop,
		}},
	}

	answer.ModelVersion = "gemini-2.5-flash-002"
	if got := translateResponse(answer, "gemini-2.5-flash").Model; got != "gemini-2.5-flash-002" {
		t.Errorf("Model = %q, want the version the provider reported", got)
	}

	// A provider that reports nothing leaves the configured id, which is still
	// better than nothing at all.
	answer.ModelVersion = ""
	if got := translateResponse(answer, "gemini-2.5-flash").Model; got != "gemini-2.5-flash" {
		t.Errorf("Model = %q, want the configured id as the fallback", got)
	}
}

// GOOGLE, not GEMINI: the price catalogue spells Gemini's vendor that way, and a
// family that does not match leaves every call here unpriced.
func TestProviderNamesTheVendorFamily(t *testing.T) {
	if got := (&Connector{}).Provider(); got != "GOOGLE" {
		t.Errorf("Provider() = %q, want GOOGLE", got)
	}
	var _ core.LLMProvider = (*Connector)(nil)
}
