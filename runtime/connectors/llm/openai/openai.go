// Package openai provides the "llm-openai" connector: a configured OpenAI
// Chat Completions client that satisfies core.LLMClient so the AI flow elements
// can drive it interchangeably with the other providers. It translates the
// provider-agnostic core.LLM* DTOs to and from the OpenAI SDK types on each
// Complete call.
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
	// Overrides the API endpoint (for proxies, Azure, or OpenAI-compatible servers).
	BaseURL string `json:"baseURL" octo:"label=Base URL"`
}

// Connector is a configured OpenAI client that AI elements call through. It is
// safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    sdk.Client
	model     string
	maxTokens int
}

var (
	_ core.Connector   = (*Connector)(nil)
	_ core.LLMClient   = (*Connector)(nil)
	_ core.EmbedClient = (*Connector)(nil)
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

	opts := []option.RequestOption{option.WithAPIKey(set.APIKey)}
	if set.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(set.BaseURL))
	}
	c.client = sdk.NewClient(opts...)

	slog.Info("llm-openai connector started",
		"connector", config.Name,
		"model", c.model,
		"maxTokens", c.maxTokens,
	)
	return nil
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Complete runs one Chat Completions turn, translating the request to SDK params
// and the response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	msgs, err := toMessages(req)
	if err != nil {
		return nil, err
	}
	tools, err := toTools(req.Tools)
	if err != nil {
		return nil, err
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
	if len(tools) > 0 {
		params.Tools = tools
		if choice, ok := toToolChoice(req.ToolChoice); ok {
			params.ToolChoice = choice
		}
	}

	cc, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-openai complete: %w", err)
	}
	return translateResponse(cc)
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
	return translateEmbedResponse(resp, len(req.Input))
}

// translateEmbedResponse reorders the SDK's index-tagged embeddings back into
// request order and converts each to float32.
func translateEmbedResponse(resp *sdk.CreateEmbeddingResponse, want int) (*core.EmbedResponse, error) {
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
	return &core.EmbedResponse{Vectors: vectors}, nil
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
func translateResponse(cc *sdk.ChatCompletion) (*core.LLMResponse, error) {
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
	}
	resp.Raw = core.LLMMessage{Role: core.LLMRoleAssistant, Text: message.Content, ToolCalls: calls}
	return resp, nil
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
