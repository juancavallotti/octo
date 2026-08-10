// Package anthropic provides the "llm-anthropic" connector: a configured
// Anthropic Messages client that satisfies core.LLMClient so the AI flow
// elements (ai-router, ai-agent, ai-mapping, ai-retry) can drive it without
// knowing which provider is behind the name they reference.
//
// The connector concentrates provider policy: API key, model, and the default
// response token cap. It translates the provider-agnostic core.LLM* DTOs to and
// from the Anthropic SDK types on each Complete call.
//
// Extended thinking is off unless the thinking setting asks for it, so a
// connector that does not mention it behaves exactly as it always has. Whether or
// not it is on, reasoning blocks a response carries are captured and echoed back
// on the next turn, which the provider requires of any assistant turn that has
// them.
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterConnector("llm-anthropic", func() core.Connector {
		return &Connector{}
	})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "llm-anthropic",
		Label:    "Anthropic",
		Icon:     "Sparkles",
		Category: "llm",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

const (
	defaultModel     = "claude-sonnet-4-6"
	defaultMaxTokens = 4096
)

// connectorSettings is the configuration decoded from the connector's settings.
type connectorSettings struct {
	// Authenticates with the Anthropic API; source from ${ANTHROPIC_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Model id.
	Model string `json:"model" octo:"label=Model,default=claude-sonnet-4-6"`
	// Default response token cap; a request may override it.
	MaxTokens int `json:"maxTokens" octo:"label=Max tokens,default=4096"`
	// Extended thinking: off, adaptive (the model decides how much), or budgeted.
	Thinking string `json:"thinking" octo:"label=Thinking,type=enum,enum=off|adaptive|budgeted,default=off"`
	// Thinking token budget, used when thinking is budgeted. At least 1024, below maxTokens.
	ThinkingBudget int `json:"thinkingBudget" octo:"label=Thinking budget"`
	// Overrides the API endpoint (for proxies or testing).
	BaseURL string `json:"baseURL" octo:"label=Base URL"`
}

const (
	thinkingOff      = "off"
	thinkingAdaptive = "adaptive"
	thinkingBudgeted = "budgeted"
	// minThinkingBudget is the provider's floor for an explicit budget.
	minThinkingBudget = 1024
)

// Connector is a configured Anthropic client that AI elements call through. It
// is safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    sdk.Client
	model     string
	maxTokens int64
	thinking  sdk.ThinkingConfigParamUnion
}

// compile-time checks that the connector is both a lifecycle connector and an
// LLM client.
var (
	_ core.Connector       = (*Connector)(nil)
	_ core.LLMClient       = (*Connector)(nil)
	_ core.LLMStreamClient = (*Connector)(nil)
)

// Start parses the settings, validates the API key, and builds the client so a
// bad configuration fails at startup rather than on first request.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return fmt.Errorf("llm-anthropic connector %q: apiKey is required", config.Name)
	}

	c.model = set.Model
	if c.model == "" {
		c.model = defaultModel
	}
	c.maxTokens = int64(set.MaxTokens)
	if c.maxTokens <= 0 {
		c.maxTokens = defaultMaxTokens
	}

	thinking, err := toThinking(set, c.maxTokens)
	if err != nil {
		return fmt.Errorf("llm-anthropic connector %q: %w", config.Name, err)
	}
	c.thinking = thinking

	opts := []option.RequestOption{option.WithAPIKey(set.APIKey)}
	if set.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(set.BaseURL))
	}
	c.client = sdk.NewClient(opts...)

	slog.Info("llm-anthropic connector started",
		"connector", config.Name,
		"model", c.model,
		"maxTokens", c.maxTokens,
		"thinking", set.Thinking,
	)
	return nil
}

// toThinking builds the thinking config from the settings. The budget is checked
// here so a value the API would reject fails at startup rather than on the first
// request; the SDK performs no client-side validation of either bound.
func toThinking(set connectorSettings, maxTokens int64) (sdk.ThinkingConfigParamUnion, error) {
	switch set.Thinking {
	case "", thinkingOff:
		// Send nothing, leaving the model on its own default. This is the zero-config
		// behaviour, so enabling thinking is always a deliberate act.
		return sdk.ThinkingConfigParamUnion{}, nil
	case thinkingAdaptive:
		// The adaptive variant rather than the enabled one: the SDK deprecates enabled
		// for newer models and prints a warning to stderr on every request that uses
		// it. There is no constructor for adaptive, so it is built literally.
		return sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{}}, nil
	case thinkingBudgeted:
		budget := int64(set.ThinkingBudget)
		switch {
		case budget < minThinkingBudget:
			return sdk.ThinkingConfigParamUnion{}, fmt.Errorf(
				"thinkingBudget must be at least %d when thinking is %q, got %d",
				minThinkingBudget, thinkingBudgeted, budget)
		case budget >= maxTokens:
			// The budget counts towards max_tokens, so it has to leave room for an answer.
			return sdk.ThinkingConfigParamUnion{}, fmt.Errorf(
				"thinkingBudget %d must be below maxTokens %d", budget, maxTokens)
		}
		return sdk.ThinkingConfigParamOfEnabled(budget), nil
	default:
		return sdk.ThinkingConfigParamUnion{}, fmt.Errorf(
			"thinking must be %q, %q or %q, got %q",
			thinkingOff, thinkingAdaptive, thinkingBudgeted, set.Thinking)
	}
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Provider names the vendor family, in the vocabulary the price catalogue uses.
// It is fixed rather than derived from baseURL: pointing this connector at a
// proxy does not change whose API it speaks or how that API counts cached
// tokens, which is what the figure downstream depends on.
func (c *Connector) Provider() string { return core.ProviderAnthropic }

// Complete runs one Messages turn, translating the request to SDK params and the
// response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, err
	}
	message, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-anthropic complete: %w", err)
	}
	return translateResponse(message, c.model), nil
}

// Stream runs one Messages turn over the streaming endpoint, reporting content as
// it arrives and returning the same response Complete would have.
//
// The SDK's own accumulator folds every event back into the message type
// translateResponse already consumes, so the streamed and blocking paths converge
// on one translation rather than two that could drift.
func (c *Connector) Stream(
	ctx context.Context, req core.LLMRequest, on func(core.LLMStreamEvent) error,
) (*core.LLMResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, err
	}

	stream := c.client.Messages.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	var acc sdk.Message
	for stream.Next() {
		event := stream.Current()
		if accErr := acc.Accumulate(event); accErr != nil {
			return nil, fmt.Errorf("llm-anthropic stream: %w", accErr)
		}
		// The accumulator carries the message_delta's output token total but not its
		// breakdown, so thinking tokens would be reported on the blocking path and
		// silently missing on this one. Copy them over to keep the two identical.
		if event.Type == "message_delta" {
			acc.Usage.OutputTokensDetails = event.Usage.OutputTokensDetails
		}
		// Accumulate first: an input_json_delta needs the tool name and id the
		// content_block_start already opened at its index.
		if emitErr := emitEvent(&acc, event, on); emitErr != nil {
			return nil, emitErr
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		return nil, fmt.Errorf("llm-anthropic stream: %w", streamErr)
	}
	return translateResponse(&acc, c.model), nil
}

// emitEvent maps one SDK stream event onto the canonical vocabulary.
//
// Only content deltas map. The terminal events carry nothing a caller cannot read
// off the returned response, and a signature_delta is bookkeeping the accumulator
// consumes on the caller's behalf.
func emitEvent(acc *sdk.Message, event sdk.MessageStreamEventUnion, on func(core.LLMStreamEvent) error) error {
	if event.Type != "content_block_delta" {
		return nil
	}
	index := int(event.Index)
	switch event.Delta.Type {
	case "text_delta":
		return on(core.LLMStreamEvent{Kind: core.LLMStreamText, Text: event.Delta.Text, Index: index})
	case "thinking_delta":
		return on(core.LLMStreamEvent{Kind: core.LLMStreamThinking, Text: event.Delta.Thinking, Index: index})
	case "signature_delta":
		return nil
	case "input_json_delta":
		ev := core.LLMStreamEvent{Kind: core.LLMStreamToolInput, Text: event.Delta.PartialJSON, Index: index}
		if index >= 0 && index < len(acc.Content) {
			ev.Tool = acc.Content[index].Name
			ev.ToolCallID = acc.Content[index].ID
		}
		return on(ev)
	default:
		// A delta variant this connector does not know — most likely one the provider
		// grew after it was written. Passing it through as custom is what lets the
		// canonical set stay closed: the event still reaches consumers, and the
		// vocabulary does not have to grow to meet every provider addition.
		ev := core.LLMStreamEvent{Kind: core.LLMStreamCustom, Name: event.Delta.Type, Index: index}
		if raw, err := json.Marshal(event.Delta); err == nil {
			ev.Text = string(raw)
		}
		// A payload that will not marshal still leaves the name worth reporting.
		return on(ev)
	}
}

// params builds the SDK request shared by Complete and Stream, so the two paths
// cannot drift in what they ask the model for.
func (c *Connector) params(req core.LLMRequest) (sdk.MessageNewParams, error) {
	msgs, err := toMessages(req.Messages)
	if err != nil {
		return sdk.MessageNewParams{}, err
	}
	tools, err := toTools(req.Tools)
	if err != nil {
		return sdk.MessageNewParams{}, err
	}

	maxTokens := c.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = int64(req.MaxTokens)
	}

	params := sdk.MessageNewParams{
		Model:     c.model,
		MaxTokens: maxTokens,
		Messages:  msgs,
	}
	if strings.TrimSpace(req.System) != "" {
		params.System = []sdk.TextBlockParam{{Text: req.System}}
	}
	if len(tools) > 0 {
		params.Tools = tools
		if choice, ok := toToolChoice(req.ToolChoice); ok {
			params.ToolChoice = choice
		}
	}
	c.applyThinking(&params, req.ToolChoice)
	return params, nil
}

// toMessages converts the conversation to SDK message params. Assistant turns
// carry their tool calls as tool_use blocks; tool turns become a user message of
// tool_result blocks, per the Anthropic convention.
func toMessages(msgs []core.LLMMessage) ([]sdk.MessageParam, error) {
	out := make([]sdk.MessageParam, 0, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case core.LLMRoleUser:
			out = append(out, sdk.NewUserMessage(sdk.NewTextBlock(m.Text)))
		case core.LLMRoleAssistant:
			out = append(out, sdk.NewAssistantMessage(assistantBlocks(m)...))
		case core.LLMRoleTool:
			blocks := make([]sdk.ContentBlockParamUnion, 0, len(m.ToolResults))
			for _, tr := range m.ToolResults {
				blocks = append(blocks, sdk.NewToolResultBlock(tr.ToolCallID, tr.Content, tr.IsError))
			}
			out = append(out, sdk.NewUserMessage(blocks...))
		default:
			return nil, fmt.Errorf("llm-anthropic: unknown message role %q at index %d", m.Role, i)
		}
	}
	return out, nil
}

// assistantBlocks builds one assistant turn's content: thinking, then text, then
// tool_use.
//
// Thinking leads, in the order the model produced it, because the server validates
// the thinking runs of an echoed turn — a block that went missing merges two runs
// into one it cannot verify, and a block that moved splits one. Either way the
// request is rejected, so the order here is load-bearing rather than cosmetic.
func assistantBlocks(m core.LLMMessage) []sdk.ContentBlockParamUnion {
	blocks := make([]sdk.ContentBlockParamUnion, 0, len(m.Thinking)+1+len(m.ToolCalls))
	for _, tb := range m.Thinking {
		if len(tb.Redacted) > 0 {
			blocks = append(blocks, sdk.NewRedactedThinkingBlock(string(tb.Redacted)))
			continue
		}
		blocks = append(blocks, sdk.NewThinkingBlock(tb.Signature, tb.Text))
	}
	if m.Text != "" {
		blocks = append(blocks, sdk.NewTextBlock(m.Text))
	}
	for _, tc := range m.ToolCalls {
		input := tc.Input
		if len(input) == 0 {
			input = json.RawMessage("{}")
		}
		blocks = append(blocks, sdk.NewToolUseBlock(tc.ID, input, tc.Name))
	}
	return blocks
}

// toTools converts tool definitions to SDK tool params, decoding each JSON Schema
// into the SDK's input-schema shape.
func toTools(tools []core.LLMTool) ([]sdk.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]sdk.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		var schema sdk.ToolInputSchemaParam
		if len(t.InputSchema) > 0 {
			if err := schema.UnmarshalJSON(t.InputSchema); err != nil {
				return nil, fmt.Errorf("llm-anthropic: tool %q input schema: %w", t.Name, err)
			}
		}
		tp := sdk.ToolParam{Name: t.Name, InputSchema: schema}
		if t.Description != "" {
			tp.Description = param.NewOpt(t.Description)
		}
		out = append(out, sdk.ToolUnionParam{OfTool: &tp})
	}
	return out, nil
}

// applyThinking sets the configured thinking on the request, unless the request
// cannot carry it. Both exclusions are a 400 from the server that no SDK checks,
// and in both thinking is the side that yields: it is an optimization, while the
// thing it conflicts with is what the caller actually asked for.
//
// A forced tool choice is the first. That is what lets a single thinking-enabled
// connector still serve an ai-router, which forces a choice on every call.
//
// The second is a max_tokens too small to hold the budget. toThinking checks that
// at startup, but only against the connector's own default: any AI block may
// override maxTokens per call, so a budget validated at startup can still exceed
// what a particular request allows. Startup cannot see that request, which is why
// the check has to happen again here.
func (c *Connector) applyThinking(params *sdk.MessageNewParams, tc core.LLMToolChoice) {
	if !c.thinkingEnabled() {
		return
	}
	if forcesTool(tc) {
		slog.Debug("llm-anthropic omitting thinking: the request forces a tool choice",
			"model", c.model, "mode", tc.Mode)
		return
	}
	if budget := c.budgetTokens(); budget > 0 && budget >= params.MaxTokens {
		// Warn rather than debug: unlike a forced choice, which is structural and
		// expected, this is a configuration mismatch the user can fix.
		slog.Warn("llm-anthropic omitting thinking: the request's maxTokens cannot fit the thinking budget",
			"model", c.model, "maxTokens", params.MaxTokens, "thinkingBudget", budget)
		return
	}
	params.Thinking = c.thinking
}

// budgetTokens reports the configured thinking budget, or 0 when there is no
// fixed one to check. Adaptive carries no budget: the model decides within
// whatever max_tokens the request sets, so it fits by construction.
func (c *Connector) budgetTokens() int64 {
	if c.thinking.OfEnabled == nil {
		return 0
	}
	return c.thinking.OfEnabled.BudgetTokens
}

// thinkingEnabled reports whether a thinking config was configured at all.
func (c *Connector) thinkingEnabled() bool {
	return c.thinking.OfEnabled != nil || c.thinking.OfAdaptive != nil
}

// forcesTool reports whether the choice compels a tool call. Those are the modes
// extended thinking cannot be combined with; none and auto are both fine.
func forcesTool(tc core.LLMToolChoice) bool {
	return tc.Mode == core.LLMToolChoiceAny || tc.Mode == core.LLMToolChoiceTool
}

// toToolChoice maps the agnostic tool choice to the SDK union. The second return
// is false for the auto (zero) mode, signalling the caller to leave it unset.
func toToolChoice(tc core.LLMToolChoice) (sdk.ToolChoiceUnionParam, bool) {
	switch tc.Mode {
	case core.LLMToolChoiceAny:
		return sdk.ToolChoiceUnionParam{OfAny: &sdk.ToolChoiceAnyParam{}}, true
	case core.LLMToolChoiceNone:
		return sdk.ToolChoiceUnionParam{OfNone: &sdk.ToolChoiceNoneParam{}}, true
	case core.LLMToolChoiceTool:
		return sdk.ToolChoiceParamOfTool(tc.Name), true
	default:
		return sdk.ToolChoiceUnionParam{}, false
	}
}

// translateResponse folds the SDK message into the agnostic response, collecting
// text, thinking and tool_use blocks and mapping the stop reason. Thinking is kept
// out of Text and carried on Raw, so reasoning cannot reach a caller that folds
// the answer into a message body.
func translateResponse(message *sdk.Message, configuredModel string) *core.LLMResponse {
	var text strings.Builder
	var calls []core.LLMToolCall
	var thinking []core.LLMThinkingBlock
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "thinking":
			thinking = append(thinking, core.LLMThinkingBlock{
				Text:      block.Thinking,
				Signature: block.Signature,
			})
		case "redacted_thinking":
			thinking = append(thinking, core.LLMThinkingBlock{Redacted: []byte(block.Data)})
		case "tool_use":
			calls = append(calls, core.LLMToolCall{
				ID:    block.ID,
				Name:  block.Name,
				Input: block.Input,
			})
		}
	}
	resp := &core.LLMResponse{
		Text:       text.String(),
		ToolCalls:  calls,
		StopReason: mapStopReason(string(message.StopReason)),
		Usage:      translateUsage(message.Usage),
		Model:      servedBy(message.Model, configuredModel),
	}
	resp.Raw = core.LLMMessage{
		Role:      core.LLMRoleAssistant,
		Text:      resp.Text,
		Thinking:  thinking,
		ToolCalls: calls,
	}
	return resp
}

// servedBy is the model that answered, preferring what the provider echoed over
// what the connector asked for. The two differ whenever a configured alias
// resolves to a dated snapshot, and it is the snapshot that was billed.
func servedBy(reported, configured string) string {
	if reported != "" {
		return reported
	}
	return configured
}

// translateUsage converts the SDK's token counts, reporting nil when the response
// carried none. OutputTokens passes through unchanged: Anthropic documents it as
// "the inclusive, authoritative total used for billing" with thinking already
// counted inside, which is the convention core.LLMUsage adopts.
func translateUsage(u sdk.Usage) *core.LLMUsage {
	// Cached input counts separately from InputTokens, which reports only the
	// uncached remainder, so a response can carry real usage with both of the
	// other two at zero.
	if u.InputTokens == 0 && u.OutputTokens == 0 && u.CacheReadInputTokens == 0 {
		return nil
	}
	return &core.LLMUsage{
		InputTokens:    int(u.InputTokens),
		OutputTokens:   int(u.OutputTokens),
		ThinkingTokens: int(u.OutputTokensDetails.ThinkingTokens),
		CachedTokens:   int(u.CacheReadInputTokens),
	}
}

// mapStopReason maps the Anthropic stop reason wire value to the agnostic one.
// stop_sequence and pause_turn fold into end_turn (a normal completion).
func mapStopReason(wire string) core.LLMStopReason {
	switch wire {
	case "tool_use":
		return core.LLMStopToolUse
	case "max_tokens":
		return core.LLMStopMaxTokens
	case "refusal":
		return core.LLMStopRefusal
	default:
		return core.LLMStopEndTurn
	}
}
