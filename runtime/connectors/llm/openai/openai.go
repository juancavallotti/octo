// Package openai provides the "llm-openai" connector: a configured OpenAI
// Chat Completions client that satisfies core.LLMClient so the AI flow elements
// can drive it interchangeably with the other providers. It translates the
// provider-agnostic core.LLM* DTOs to and from the OpenAI SDK types on each
// Complete call.
//
// Chat Completions exposes no reasoning content — not on a delta, not on the
// finished message — so core.LLMMessage.Thinking is always empty here and an
// echoed turn carries none. Reasoning is visible only as a token count. That is
// an API limitation, not an omission: reasoning summaries live on the Responses
// API, which this connector does not use.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	sdk "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/shared"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterConnector("llm-openai", func() core.Connector {
		return &Connector{}
	})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "llm-openai",
		Label:    "OpenAI",
		Icon:     "Sparkles",
		Category: "llm",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

const defaultModel = "gpt-5.4"

// connectorSettings is the configuration decoded from the connector's settings.
type connectorSettings struct {
	// Authenticates with the OpenAI API; source from ${OPENAI_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Model id.
	Model string `json:"model" octo:"label=Model,default=gpt-5.4"`
	// Default response token cap (0 = the model default); a request may override it.
	MaxTokens int `json:"maxTokens" octo:"label=Max tokens"`
	// How much reasoning effort a reasoning-capable model spends. "none" turns it
	// off explicitly; "default" leaves the field off the request and lets the model
	// choose, which for a reasoning model is not the same thing as none.
	//nolint:lll // a struct tag cannot be wrapped, and the enum has to list every option
	Reasoning string `json:"reasoning" octo:"label=Reasoning,type=enum,enum=none|minimal|low|medium|high|default,default=none"`
	// Overrides the API endpoint (for proxies, Azure, or OpenAI-compatible servers).
	BaseURL string `json:"baseURL" octo:"label=Base URL"`
}

const (
	// reasoningNone asks the model for no reasoning at all, on the wire.
	reasoningNone = "none"
	// reasoningDefault omits reasoning_effort so the model applies its own.
	reasoningDefault = "default"
)

// Connector is a configured OpenAI client that AI elements call through. It is
// safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    sdk.Client
	model     string
	maxTokens int
	reasoning shared.ReasoningEffort
}

var (
	_ core.Connector       = (*Connector)(nil)
	_ core.LLMClient       = (*Connector)(nil)
	_ core.LLMStreamClient = (*Connector)(nil)
	_ core.EmbedClient     = (*Connector)(nil)
)

// Start parses the settings, validates the API key, and builds the client so a
// bad configuration fails at startup rather than on first request.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return fmt.Errorf("llm-openai connector %q: apiKey is required", config.Name)
	}

	c.model = set.Model
	if c.model == "" {
		c.model = defaultModel
	}
	c.maxTokens = set.MaxTokens

	reasoning, err := toReasoningEffort(set.Reasoning)
	if err != nil {
		return fmt.Errorf("llm-openai connector %q: %w", config.Name, err)
	}
	c.reasoning = reasoning

	opts := []option.RequestOption{option.WithAPIKey(set.APIKey)}
	if set.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(set.BaseURL))
	}
	c.client = sdk.NewClient(opts...)

	slog.Info("llm-openai connector started",
		"connector", config.Name,
		"model", c.model,
		"maxTokens", c.maxTokens,
		"reasoning", set.Reasoning,
	)
	return nil
}

// toReasoningEffort validates the setting at startup, returning the empty effort
// for off so the request carries no reasoning_effort and the model's own default
// applies. This steers how many reasoning tokens the model spends; it does not
// make reasoning observable, which Chat Completions never does.
func toReasoningEffort(effort string) (shared.ReasoningEffort, error) {
	switch effort {
	// Unset and "off" both mean no reasoning, and both now say so on the wire
	// rather than by omission. Omitting the field asks a reasoning model for its
	// own default, which is not none — and a non-none default cannot be combined
	// with function tools on /v1/chat/completions, so every agent built on such a
	// model failed its first turn with a 400 naming reasoning_effort.
	case "", reasoningNone:
		return shared.ReasoningEffort(reasoningNone), nil
	// The old behaviour, for a model that rejects an explicit none.
	case reasoningDefault:
		return "", nil
	case string(shared.ReasoningEffortMinimal), string(shared.ReasoningEffortLow),
		string(shared.ReasoningEffortMedium), string(shared.ReasoningEffortHigh):
		return shared.ReasoningEffort(effort), nil
	default:
		return "", fmt.Errorf(
			"reasoning must be one of none, minimal, low, medium, high, default, got %q", effort)
	}
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Provider names the vendor family, in the vocabulary the price catalogue uses.
// It is fixed rather than derived from baseURL: this connector speaks the OpenAI
// API and its token accounting whether it is pointed at OpenAI, Azure, or any
// other compatible server, and that accounting is what the figure downstream
// depends on.
func (c *Connector) Provider() string { return core.ProviderOpenAI }

// Complete runs one Chat Completions turn, translating the request to SDK params
// and the response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, err
	}
	cc, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-openai complete: %w", err)
	}
	return translateResponse(cc, c.model)
}

// Stream runs one Chat Completions turn over the streaming endpoint, reporting
// content as it arrives and returning the same response Complete would have.
//
// Usage and the finish reason are taken from the chunks directly rather than from
// the SDK accumulator, which sums usage and overwrites the finish reason on every
// chunk carrying choices. Against real OpenAI both are harmless — non-final chunks
// report no usage, and the usage chunk carries no choices — but this connector
// advertises baseURL for OpenAI-compatible servers, where a server that echoes
// usage on every chunk would be multiplied and a late chunk would clear the finish
// reason back to a plain end-of-turn.
func (c *Connector) Stream(
	ctx context.Context, req core.LLMRequest, on func(core.LLMStreamEvent) error,
) (*core.LLMResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, err
	}
	params.StreamOptions = sdk.ChatCompletionStreamOptionsParam{IncludeUsage: param.NewOpt(true)}

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	var (
		acc    sdk.ChatCompletionAccumulator
		usage  sdk.CompletionUsage
		finish string
	)
	for stream.Next() {
		chunk := stream.Current()
		acc.AddChunk(chunk)
		if chunk.Usage.PromptTokens != 0 || chunk.Usage.CompletionTokens != 0 {
			usage = chunk.Usage
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].FinishReason != "" {
			finish = chunk.Choices[0].FinishReason
		}
		// Accumulate first: a fragment after the first carries no name or id, so the
		// call it belongs to has to be read off what the accumulator has opened.
		if emitErr := emitChunk(&acc, chunk, on); emitErr != nil {
			return nil, emitErr
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		return nil, fmt.Errorf("llm-openai stream: %w", streamErr)
	}

	cc := acc.ChatCompletion
	cc.Usage = usage
	if len(cc.Choices) > 0 {
		cc.Choices[0].FinishReason = finish
	}
	return translateResponse(&cc, c.model)
}

// emitChunk maps one chunk onto the canonical vocabulary. The final usage chunk
// carries no choices and so produces nothing.
//
// Only the first choice is streamed, matching translateResponse. The connector
// never asks for more than one — it does not set n — so the rest is a case that
// cannot arise today; a change that starts requesting them has to come back here
// rather than silently dropping their events.
func emitChunk(
	acc *sdk.ChatCompletionAccumulator, chunk sdk.ChatCompletionChunk, on func(core.LLMStreamEvent) error,
) error {
	if len(chunk.Choices) == 0 {
		return nil
	}
	delta := chunk.Choices[0].Delta
	if delta.Content != "" {
		if err := on(core.LLMStreamEvent{Kind: core.LLMStreamText, Text: delta.Content}); err != nil {
			return err
		}
	}
	if delta.Refusal != "" {
		// A refusal is content, but it is not the answer, so it is not text. There is
		// no canonical kind for it and inventing one would grow the vocabulary for a
		// single provider, which is what custom exists to avoid.
		if err := on(core.LLMStreamEvent{
			Kind: core.LLMStreamCustom, Name: "refusal", Text: delta.Refusal,
		}); err != nil {
			return err
		}
	}
	for _, tc := range delta.ToolCalls {
		if tc.Function.Arguments == "" {
			continue
		}
		// The index is the server's, and it indexes a slice. An upper bound alone
		// would still panic on a negative one, which matters here because baseURL
		// points this connector at OpenAI-compatible servers whose framing is not
		// OpenAI's to guarantee.
		idx := int(tc.Index)
		if idx < 0 {
			continue
		}
		ev := core.LLMStreamEvent{
			Kind:  core.LLMStreamToolInput,
			Text:  tc.Function.Arguments,
			Index: idx,
		}
		if len(acc.Choices) > 0 && idx < len(acc.Choices[0].Message.ToolCalls) {
			call := acc.Choices[0].Message.ToolCalls[idx]
			ev.Tool, ev.ToolCallID = call.Function.Name, call.ID
		}
		if err := on(ev); err != nil {
			return err
		}
	}
	return nil
}

// params builds the SDK request shared by Complete and Stream, so the two paths
// cannot drift in what they ask the model for.
func (c *Connector) params(req core.LLMRequest) (sdk.ChatCompletionNewParams, error) {
	msgs, err := toMessages(req)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, err
	}
	tools, err := toTools(req.Tools)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, err
	}

	params := sdk.ChatCompletionNewParams{
		Model:    c.model,
		Messages: msgs,
	}
	maxTokens := c.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}
	if maxTokens > 0 {
		params.MaxCompletionTokens = param.NewOpt(int64(maxTokens))
	}
	if c.reasoning != "" {
		params.ReasoningEffort = c.reasoning
	}
	if len(tools) > 0 {
		params.Tools = tools
		if choice, ok := toToolChoice(req.ToolChoice); ok {
			params.ToolChoice = choice
		}
	}
	return params, nil
}

// Embed embeds a batch of texts in one request, translating the SDK's
// index-tagged results back into request order — the SDK documents input as an
// array but doesn't promise the response preserves that order.
func (c *Connector) Embed(ctx context.Context, req core.EmbedRequest) (*core.EmbedResponse, error) {
	if len(req.Input) == 0 {
		return nil, fmt.Errorf("llm-openai embed: input is required")
	}

	params := sdk.EmbeddingNewParams{
		Input: sdk.EmbeddingNewParamsInputUnion{OfArrayOfStrings: req.Input},
		Model: req.Model,
	}
	if req.Dimensions > 0 {
		params.Dimensions = param.NewOpt(int64(req.Dimensions))
	}

	resp, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-openai embed: %w", err)
	}
	return translateEmbedResponse(resp, len(req.Input), req.Model)
}

// translateEmbedResponse reorders the SDK's index-tagged embeddings back into
// request order and converts each to float32, carrying the model that served the
// call and what it charged for it.
func translateEmbedResponse(
	resp *sdk.CreateEmbeddingResponse, want int, requestedModel string,
) (*core.EmbedResponse, error) {
	if len(resp.Data) != want {
		return nil, fmt.Errorf("llm-openai embed: response had %d embeddings, want %d", len(resp.Data), want)
	}
	vectors := make([][]float32, want)
	for _, d := range resp.Data {
		if d.Index < 0 || int(d.Index) >= want {
			return nil, fmt.Errorf("llm-openai embed: response embedding index %d out of range", d.Index)
		}
		vectors[d.Index] = toFloat32(d.Embedding)
	}
	return &core.EmbedResponse{
		Vectors: vectors,
		Usage:   translateEmbedUsage(resp.Usage),
		Model:   servedBy(resp.Model, requestedModel),
	}, nil
}

// translateEmbedUsage converts the SDK's token count, reporting nil when the
// response carried none. Only the prompt total is taken: an embedding produces no
// output, so the SDK's TotalTokens is the same number under another name.
func translateEmbedUsage(u sdk.CreateEmbeddingResponseUsage) *core.EmbedUsage {
	if u.PromptTokens == 0 {
		return nil
	}
	return &core.EmbedUsage{InputTokens: int(u.PromptTokens)}
}

// toFloat32 narrows the SDK's float64 embedding values to the runtime's float32
// vector representation.
func toFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}

// toMessages converts the conversation to SDK message params, prepending the
// system prompt as a system message. Assistant turns carry their tool calls;
// each tool result becomes its own tool message.
func toMessages(req core.LLMRequest) ([]sdk.ChatCompletionMessageParamUnion, error) {
	out := make([]sdk.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		out = append(out, sdk.SystemMessage(req.System))
	}
	for i, m := range req.Messages {
		switch m.Role {
		case core.LLMRoleUser:
			out = append(out, sdk.UserMessage(m.Text))
		case core.LLMRoleAssistant:
			asst := sdk.ChatCompletionAssistantMessageParam{}
			if m.Text != "" {
				asst.Content.OfString = param.NewOpt(m.Text)
			}
			for _, tc := range m.ToolCalls {
				asst.ToolCalls = append(asst.ToolCalls, sdk.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &sdk.ChatCompletionMessageFunctionToolCallParam{
						ID: tc.ID,
						Function: sdk.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      tc.Name,
							Arguments: string(tc.Input),
						},
					},
				})
			}
			out = append(out, sdk.ChatCompletionMessageParamUnion{OfAssistant: &asst})
		case core.LLMRoleTool:
			for _, tr := range m.ToolResults {
				content := tr.Content
				if tr.IsError {
					content = "ERROR: " + tr.Content
				}
				out = append(out, sdk.ToolMessage(content, tr.ToolCallID))
			}
		default:
			return nil, fmt.Errorf("llm-openai: unknown message role %q at index %d", m.Role, i)
		}
	}
	return out, nil
}

// toTools converts tool definitions to SDK function tools, decoding each JSON
// Schema into the SDK's function-parameters map.
func toTools(tools []core.LLMTool) ([]sdk.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]sdk.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		var params shared.FunctionParameters
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("llm-openai: tool %q input schema: %w", t.Name, err)
			}
		}
		fn := shared.FunctionDefinitionParam{Name: t.Name, Parameters: params}
		if t.Description != "" {
			fn.Description = param.NewOpt(t.Description)
		}
		out = append(out, sdk.ChatCompletionFunctionTool(fn))
	}
	return out, nil
}

// toToolChoice maps the agnostic tool choice to the SDK union. The second return
// is false for the auto (zero) mode, signalling the caller to leave it unset.
func toToolChoice(tc core.LLMToolChoice) (sdk.ChatCompletionToolChoiceOptionUnionParam, bool) {
	switch tc.Mode {
	case core.LLMToolChoiceAny:
		return sdk.ChatCompletionToolChoiceOptionUnionParam{OfAuto: param.NewOpt("required")}, true
	case core.LLMToolChoiceNone:
		return sdk.ChatCompletionToolChoiceOptionUnionParam{OfAuto: param.NewOpt("none")}, true
	case core.LLMToolChoiceTool:
		return sdk.ToolChoiceOptionFunctionToolChoice(
			sdk.ChatCompletionNamedToolChoiceFunctionParam{Name: tc.Name}), true
	default:
		return sdk.ChatCompletionToolChoiceOptionUnionParam{}, false
	}
}

// translateResponse folds the first choice into the agnostic response, collecting
// text and function tool calls and mapping the finish reason.
func translateResponse(cc *sdk.ChatCompletion, configuredModel string) (*core.LLMResponse, error) {
	if len(cc.Choices) == 0 {
		return nil, fmt.Errorf("llm-openai: response had no choices")
	}
	choice := cc.Choices[0]
	message := choice.Message

	var calls []core.LLMToolCall
	for _, tc := range message.ToolCalls {
		if tc.Type != "" && tc.Type != "function" {
			continue
		}
		var input json.RawMessage
		if tc.Function.Arguments != "" {
			input = json.RawMessage(tc.Function.Arguments)
		}
		calls = append(calls, core.LLMToolCall{ID: tc.ID, Name: tc.Function.Name, Input: input})
	}

	resp := &core.LLMResponse{
		Text:       message.Content,
		ToolCalls:  calls,
		StopReason: mapFinishReason(choice.FinishReason, message.Refusal),
		Usage:      translateUsage(cc.Usage),
		Model:      servedBy(cc.Model, configuredModel),
	}
	resp.Raw = core.LLMMessage{Role: core.LLMRoleAssistant, Text: message.Content, ToolCalls: calls}
	return resp, nil
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
// carried none. CompletionTokens already counts reasoning tokens inside itself,
// matching the inclusive convention core.LLMUsage adopts.
func translateUsage(u sdk.CompletionUsage) *core.LLMUsage {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 {
		return nil
	}
	return &core.LLMUsage{
		InputTokens:    int(u.PromptTokens),
		OutputTokens:   int(u.CompletionTokens),
		ThinkingTokens: int(u.CompletionTokensDetails.ReasoningTokens),
		CachedTokens:   int(u.PromptTokensDetails.CachedTokens),
	}
}

// mapFinishReason maps the OpenAI finish reason (and a refusal) to the agnostic
// stop reason.
func mapFinishReason(reason, refusal string) core.LLMStopReason {
	if refusal != "" {
		return core.LLMStopRefusal
	}
	switch reason {
	case "tool_calls":
		return core.LLMStopToolUse
	case "length":
		return core.LLMStopMaxTokens
	case "content_filter":
		return core.LLMStopRefusal
	default:
		return core.LLMStopEndTurn
	}
}
