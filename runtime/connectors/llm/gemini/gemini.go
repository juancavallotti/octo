// Package gemini provides the "llm-gemini" connector: a configured Google Gemini
// client that satisfies core.LLMClient so the AI flow elements can drive it
// interchangeably with the other providers. It translates the provider-agnostic
// core.LLM* DTOs to and from the genai SDK types on each Complete call.
//
// Gemini has no explicit tool-call IDs and returns function calls with a STOP
// finish reason, so this connector synthesizes the agnostic ToolCallID from the
// function name (correlating tool results by name) and reports a tool-use stop
// reason whenever the response carries function calls.
//
// Thought parts are captured onto the assistant turn's Thinking rather than its
// Text. Of those, only the thought signature is echoed back, on the function call
// where Gemini requires it; thought text is not replayed, since Gemini asks for
// the signature rather than the reasoning to continue a tool conversation.
package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"reflect"
	"strings"

	"google.golang.org/genai"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterConnector("llm-gemini", func() core.Connector {
		return &Connector{}
	})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "llm-gemini",
		Label:    "Gemini",
		Icon:     "Sparkles",
		Category: "llm",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

const defaultModel = "gemini-3.5-flash"

// connectorSettings is the configuration decoded from the connector's settings.
type connectorSettings struct {
	// Authenticates with the Gemini API; source from ${GEMINI_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Model id.
	Model string `json:"model" octo:"label=Model,default=gemini-3.5-flash"`
	// Default response token cap (0 = the model default); a request may override it.
	MaxTokens int `json:"maxTokens" octo:"label=Max tokens"`
	// Thinking: off, dynamic (the model decides how much), or budgeted.
	Thinking string `json:"thinking" octo:"label=Thinking,type=enum,enum=off|dynamic|budgeted,default=off"`
	// Thinking token budget, used when thinking is budgeted.
	ThinkingBudget int `json:"thinkingBudget" octo:"label=Thinking budget"`
	// Overrides the API endpoint (for proxies or testing).
	BaseURL string `json:"baseURL" octo:"label=Base URL"`
}

const (
	thinkingOff      = "off"
	thinkingDynamic  = "dynamic"
	thinkingBudgeted = "budgeted"
	// dynamicThinkingBudget is the provider's sentinel for "you decide".
	dynamicThinkingBudget = -1
)

// Connector is a configured Gemini client that AI elements call through. It is
// safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    *genai.Client
	model     string
	maxTokens int
	thinking  *genai.ThinkingConfig
}

var (
	_ core.Connector       = (*Connector)(nil)
	_ core.LLMClient       = (*Connector)(nil)
	_ core.LLMStreamClient = (*Connector)(nil)
	_ core.EmbedClient     = (*Connector)(nil)
)

// Start parses the settings, validates the API key, and builds the client so a
// bad configuration fails at startup rather than on first request.
func (c *Connector) Start(ctx context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return fmt.Errorf("llm-gemini connector %q: apiKey is required", config.Name)
	}

	c.model = set.Model
	if c.model == "" {
		c.model = defaultModel
	}
	c.maxTokens = set.MaxTokens

	thinking, err := toThinkingConfig(set)
	if err != nil {
		return fmt.Errorf("llm-gemini connector %q: %w", config.Name, err)
	}
	c.thinking = thinking

	cc := &genai.ClientConfig{APIKey: set.APIKey, Backend: genai.BackendGeminiAPI}
	if set.BaseURL != "" {
		cc.HTTPOptions.BaseURL = set.BaseURL
	}
	client, err := genai.NewClient(ctx, cc)
	if err != nil {
		return fmt.Errorf("llm-gemini connector %q: %w", config.Name, err)
	}
	c.client = client

	slog.Info("llm-gemini connector started",
		"connector", config.Name,
		"model", c.model,
		"maxTokens", c.maxTokens,
		"thinking", set.Thinking,
	)
	return nil
}

// toThinkingConfig builds the thinking config from the settings, returning nil for
// off so the request carries none and the model keeps whatever default it has.
//
// IncludeThoughts is set whenever thinking is on: without it a thinking model
// still reasons but returns no thought parts, so there would be nothing to
// observe or stream.
func toThinkingConfig(set connectorSettings) (*genai.ThinkingConfig, error) {
	switch set.Thinking {
	case "", thinkingOff:
		return nil, nil
	case thinkingDynamic:
		budget := int32(dynamicThinkingBudget)
		return &genai.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: &budget}, nil
	case thinkingBudgeted:
		if set.ThinkingBudget <= 0 || set.ThinkingBudget > math.MaxInt32 {
			return nil, fmt.Errorf("thinkingBudget must be between 1 and %d when thinking is %q, got %d",
				math.MaxInt32, thinkingBudgeted, set.ThinkingBudget)
		}
		budget := int32(set.ThinkingBudget) //nolint:gosec // bounds-checked above
		return &genai.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: &budget}, nil
	default:
		return nil, fmt.Errorf("thinking must be %q, %q or %q, got %q",
			thinkingOff, thinkingDynamic, thinkingBudgeted, set.Thinking)
	}
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Complete runs one GenerateContent turn, translating the request to SDK types
// and the response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	contents, cfg, err := c.request(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Models.GenerateContent(ctx, c.model, contents, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm-gemini complete: %w", err)
	}
	return translateResponse(resp), nil
}

// Stream runs one GenerateContent turn over the streaming endpoint, reporting
// content as it arrives and returning the same response Complete would have.
//
// Unlike the other two SDKs, genai ships no accumulator and each chunk is a delta
// rather than a snapshot, so the fold is written here. Folding back into the
// provider's own response shape rather than straight into the agnostic DTOs is
// what lets translateResponse stay the single translation both paths go through.
func (c *Connector) Stream(
	ctx context.Context, req core.LLMRequest, on func(core.LLMStreamEvent) error,
) (*core.LLMResponse, error) {
	contents, cfg, err := c.request(req)
	if err != nil {
		return nil, err
	}

	var fold streamFold
	for chunk, chunkErr := range c.client.Models.GenerateContentStream(ctx, c.model, contents, cfg) {
		if chunkErr != nil {
			// The SDK yields io.EOF as its ordinary end of stream, not as a failure.
			if errors.Is(chunkErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("llm-gemini stream: %w", chunkErr)
		}
		if emitErr := fold.add(chunk, on); emitErr != nil {
			return nil, emitErr
		}
	}
	return translateResponse(fold.response()), nil
}

// streamFold reassembles a streamed turn into one response.
type streamFold struct {
	parts  []*genai.Part
	finish genai.FinishReason
	usage  *genai.GenerateContentResponseUsageMetadata
	seen   bool
}

// add folds one chunk in and reports its content as canonical events.
func (f *streamFold) add(chunk *genai.GenerateContentResponse, on func(core.LLMStreamEvent) error) error {
	if chunk == nil {
		return nil
	}
	// Usage is a running snapshot rather than a delta — the opposite of the content
	// alongside it — so the last one reported wins and summing would be wrong.
	if chunk.UsageMetadata != nil {
		f.usage = chunk.UsageMetadata
	}
	if len(chunk.Candidates) == 0 {
		return nil
	}
	f.seen = true
	cand := chunk.Candidates[0]
	if cand.FinishReason != "" {
		f.finish = cand.FinishReason
	}
	if cand.Content == nil {
		return nil
	}
	for _, part := range cand.Content.Parts {
		if err := emitPart(part, f.appendPart(part), on); err != nil {
			return err
		}
	}
	return nil
}

// appendPart folds one part into the run and returns the index it occupies.
//
// Consecutive text of the same kind is merged rather than appended. Gemini splits
// a logical text run across chunks, and sometimes across parts within one chunk,
// so keeping each fragment as its own part would leave a streamed turn structurally
// different from the same turn unstreamed — one thinking block per fragment instead
// of one per run. Merging is also what makes the returned index a stable id for the
// run rather than a per-fragment counter.
func (f *streamFold) appendPart(p *genai.Part) int {
	n := len(f.parts)
	if n > 0 && p.FunctionCall == nil && p.Text != "" {
		if prev := f.parts[n-1]; prev.FunctionCall == nil && prev.Text != "" && prev.Thought == p.Thought {
			merged := *prev
			merged.Text = prev.Text + p.Text
			if len(p.ThoughtSignature) > 0 {
				merged.ThoughtSignature = p.ThoughtSignature
			}
			f.parts[n-1] = &merged
			return n - 1
		}
	}
	f.parts = append(f.parts, p)
	return n
}

// response rebuilds the turn as the provider would have returned it unstreamed. A
// stream that produced no candidate at all rebuilds as none, so translateResponse
// reads it as a refusal exactly as it would on the blocking path.
func (f *streamFold) response() *genai.GenerateContentResponse {
	out := &genai.GenerateContentResponse{UsageMetadata: f.usage}
	if !f.seen {
		return out
	}
	out.Candidates = []*genai.Candidate{{
		Content:      &genai.Content{Parts: f.parts, Role: genai.RoleModel},
		FinishReason: f.finish,
	}}
	return out
}

// emitPart maps one part onto the canonical vocabulary. index identifies the run
// the part belongs to, so the fragments of one text run share it.
//
// A function call is reported as a single tool_input carrying its complete
// arguments. Gemini never fragments them, unlike the other two providers, but a
// consumer concatenating tool_input text per call still ends up with the same
// valid JSON — which is the point of a canonical vocabulary, and better than
// staying silent because the delivery differs.
func emitPart(part *genai.Part, index int, on func(core.LLMStreamEvent) error) error {
	switch {
	case part.Thought && part.Text != "":
		if err := on(core.LLMStreamEvent{
			Kind: core.LLMStreamThinking, Text: part.Text, Index: index,
		}); err != nil {
			return err
		}
	case part.Text != "":
		if err := on(core.LLMStreamEvent{
			Kind: core.LLMStreamText, Text: part.Text, Index: index,
		}); err != nil {
			return err
		}
	}
	if fc := part.FunctionCall; fc != nil {
		args, err := json.Marshal(fc.Args)
		if err != nil {
			return fmt.Errorf("llm-gemini stream: encode call %q arguments: %w", fc.Name, err)
		}
		if err := on(core.LLMStreamEvent{
			Kind: core.LLMStreamToolInput, Text: string(args),
			Tool: fc.Name, ToolCallID: fc.Name, Index: index,
		}); err != nil {
			return err
		}
	}
	return nil
}

// request builds the contents and config shared by Complete and Stream, so the
// two paths cannot drift in what they ask the model for.
func (c *Connector) request(req core.LLMRequest) ([]*genai.Content, *genai.GenerateContentConfig, error) {
	contents, err := toContents(req.Messages)
	if err != nil {
		return nil, nil, err
	}

	cfg := &genai.GenerateContentConfig{}
	if strings.TrimSpace(req.System) != "" {
		cfg.SystemInstruction = genai.NewContentFromText(req.System, genai.RoleUser)
	}
	maxTokens := c.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}
	if maxTokens > 0 {
		if maxTokens > math.MaxInt32 {
			maxTokens = math.MaxInt32
		}
		cfg.MaxOutputTokens = int32(maxTokens) //nolint:gosec // clamped to MaxInt32 above
	}
	cfg.ThinkingConfig = c.thinking
	if tools, toolErr := toTools(req.Tools); toolErr != nil {
		return nil, nil, toolErr
	} else if tools != nil {
		cfg.Tools = tools
		if tc, ok := toToolConfig(req.ToolChoice); ok {
			cfg.ToolConfig = tc
		}
	}
	return contents, cfg, nil
}

// Embed embeds a batch of texts in one request. Gemini documents the response as
// preserving request order, so no reordering is needed (contrast OpenAI, which
// tags each embedding with an index instead of promising order).
func (c *Connector) Embed(ctx context.Context, req core.EmbedRequest) (*core.EmbedResponse, error) {
	if len(req.Input) == 0 {
		return nil, fmt.Errorf("llm-gemini embed: input is required")
	}

	contents := make([]*genai.Content, len(req.Input))
	for i, text := range req.Input {
		contents[i] = genai.NewContentFromText(text, genai.RoleUser)
	}

	cfg := &genai.EmbedContentConfig{}
	if req.Dimensions > 0 {
		if req.Dimensions > math.MaxInt32 {
			return nil, fmt.Errorf("llm-gemini embed: dimensions %d exceeds int32", req.Dimensions)
		}
		dims := int32(req.Dimensions) //nolint:gosec // bounds-checked above
		cfg.OutputDimensionality = &dims
	}

	resp, err := c.client.Models.EmbedContent(ctx, req.Model, contents, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm-gemini embed: %w", err)
	}
	if len(resp.Embeddings) != len(req.Input) {
		return nil, fmt.Errorf("llm-gemini embed: response had %d embeddings, want %d",
			len(resp.Embeddings), len(req.Input))
	}

	vectors := make([][]float32, len(resp.Embeddings))
	for i, e := range resp.Embeddings {
		vectors[i] = e.Values
	}
	return &core.EmbedResponse{Vectors: vectors}, nil
}

// toContents converts the conversation to SDK contents. Assistant turns map to
// the "model" role with function-call parts; tool turns map to a "user" role with
// function-response parts (Gemini's convention), correlated by function name.
func toContents(msgs []core.LLMMessage) ([]*genai.Content, error) {
	out := make([]*genai.Content, 0, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case core.LLMRoleUser:
			out = append(out, genai.NewContentFromText(m.Text, genai.RoleUser))
		case core.LLMRoleAssistant:
			parts := make([]*genai.Part, 0, 1+len(m.ToolCalls))
			if m.Text != "" {
				parts = append(parts, genai.NewPartFromText(m.Text))
			}
			for _, tc := range m.ToolCalls {
				part := genai.NewPartFromFunctionCall(tc.Name, argsToMap(tc.Input))
				// Replay the thought signature Gemini 3.x attaches to a function call;
				// it is required on the echoed call for the next turn to be accepted.
				part.ThoughtSignature = tc.Signature
				parts = append(parts, part)
			}
			out = append(out, genai.NewContentFromParts(parts, genai.RoleModel))
		case core.LLMRoleTool:
			parts := make([]*genai.Part, 0, len(m.ToolResults))
			for _, tr := range m.ToolResults {
				parts = append(parts, genai.NewPartFromFunctionResponse(tr.ToolCallID, responseMap(tr)))
			}
			out = append(out, genai.NewContentFromParts(parts, genai.RoleUser))
		default:
			return nil, fmt.Errorf("llm-gemini: unknown message role %q at index %d", m.Role, i)
		}
	}
	return out, nil
}

// toTools wraps the tool definitions in a single genai.Tool, passing each JSON
// Schema through verbatim as the function's parameters.
func toTools(tools []core.LLMTool) ([]*genai.Tool, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	decls := make([]*genai.FunctionDeclaration, 0, len(tools))
	for _, t := range tools {
		var schema any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
				return nil, fmt.Errorf("llm-gemini: tool %q input schema: %w", t.Name, err)
			}
		}
		decls = append(decls, &genai.FunctionDeclaration{
			Name:                 t.Name,
			Description:          t.Description,
			ParametersJsonSchema: schema,
		})
	}
	return []*genai.Tool{{FunctionDeclarations: decls}}, nil
}

// toToolConfig maps the agnostic tool choice to a function-calling config. The
// second return is false for the auto (zero) mode, leaving the SDK default.
func toToolConfig(tc core.LLMToolChoice) (*genai.ToolConfig, bool) {
	switch tc.Mode {
	case core.LLMToolChoiceAny:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
			Mode: genai.FunctionCallingConfigModeAny,
		}}, true
	case core.LLMToolChoiceNone:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
			Mode: genai.FunctionCallingConfigModeNone,
		}}, true
	case core.LLMToolChoiceTool:
		return &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
			Mode:                 genai.FunctionCallingConfigModeAny,
			AllowedFunctionNames: []string{tc.Name},
		}}, true
	default:
		return nil, false
	}
}

// translateResponse folds the first candidate into the agnostic response. Gemini
// reports STOP even when returning function calls, so the presence of calls drives
// the tool-use stop reason. The synthesized ToolCallID is the function name.
func translateResponse(resp *genai.GenerateContentResponse) *core.LLMResponse {
	usage := translateUsage(resp.UsageMetadata)
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return &core.LLMResponse{
			StopReason: core.LLMStopRefusal,
			Raw:        core.LLMMessage{Role: core.LLMRoleAssistant},
			Usage:      usage,
		}
	}
	cand := resp.Candidates[0]

	var text strings.Builder
	var calls []core.LLMToolCall
	var thinking []core.LLMThinkingBlock
	var sig []byte // most recent thought signature; may sit on a thought part before the call
	for _, part := range cand.Content.Parts {
		if len(part.ThoughtSignature) > 0 {
			sig = part.ThoughtSignature
		}
		switch {
		case part.Thought:
			// Reasoning, not the answer. Keeping it out of Text is what stops a caller
			// that folds the answer into a message body from publishing the model's
			// chain of thought.
			if part.Text != "" {
				thinking = append(thinking, core.LLMThinkingBlock{
					Text:      part.Text,
					Signature: string(part.ThoughtSignature),
				})
			}
		case part.Text != "":
			text.WriteString(part.Text)
		}
		if fc := part.FunctionCall; fc != nil {
			input, _ := json.Marshal(fc.Args)
			calls = append(calls, core.LLMToolCall{ID: fc.Name, Name: fc.Name, Input: input, Signature: sig})
			sig = nil // consumed by this call
		}
	}

	out := &core.LLMResponse{
		Text:       text.String(),
		ToolCalls:  calls,
		StopReason: mapFinishReason(cand.FinishReason, len(calls) > 0),
		Usage:      usage,
	}
	out.Raw = core.LLMMessage{
		Role:      core.LLMRoleAssistant,
		Text:      out.Text,
		Thinking:  thinking,
		ToolCalls: calls,
	}
	return out
}

// translateUsage converts the SDK's token counts, reporting nil when the response
// carried none. Gemini counts thoughts *outside* candidatesTokenCount, unlike the
// other two providers, so they are added in to keep OutputTokens meaning the same
// thing whichever provider answered.
func translateUsage(u *genai.GenerateContentResponseUsageMetadata) *core.LLMUsage {
	// An all-zero metadata block reports nil, the same as no metadata at all.
	// Usage != nil means "the provider accounted for this turn" on every connector,
	// and Gemini attaches the struct more eagerly than the others do, so without
	// this the same emptiness would read as accounting here and as none elsewhere.
	if u == nil || (u.PromptTokenCount == 0 && u.CandidatesTokenCount == 0 &&
		u.ThoughtsTokenCount == 0 && u.CachedContentTokenCount == 0) {
		return nil
	}
	return &core.LLMUsage{
		InputTokens:    int(u.PromptTokenCount),
		OutputTokens:   int(u.CandidatesTokenCount + u.ThoughtsTokenCount),
		ThinkingTokens: int(u.ThoughtsTokenCount),
		CachedTokens:   int(u.CachedContentTokenCount),
	}
}

// mapFinishReason maps the Gemini finish reason to the agnostic stop reason. When
// the response carries function calls it is always tool-use, since Gemini reports
// STOP in that case.
func mapFinishReason(reason genai.FinishReason, hasCalls bool) core.LLMStopReason {
	if hasCalls {
		return core.LLMStopToolUse
	}
	switch reason {
	case genai.FinishReasonMaxTokens:
		return core.LLMStopMaxTokens
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonProhibitedContent:
		return core.LLMStopRefusal
	default:
		return core.LLMStopEndTurn
	}
}

// argsToMap decodes a tool call's JSON arguments into the map shape Gemini wants.
func argsToMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]any{}
	}
	return m
}

// responseMap wraps a tool result as the map shape Gemini wants for a function
// response: an object is passed through, anything else is wrapped, and an error
// result is reported under "error".
func responseMap(tr core.LLMToolResult) map[string]any {
	if tr.IsError {
		return map[string]any{"error": tr.Content}
	}
	var v any
	if err := json.Unmarshal([]byte(tr.Content), &v); err == nil {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return map[string]any{"result": v}
	}
	return map[string]any{"result": tr.Content}
}
