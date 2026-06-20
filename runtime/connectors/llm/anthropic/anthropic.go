// Package anthropic provides the "llm-anthropic" connector: a configured
// Anthropic Messages client that satisfies core.LLMClient so the AI flow
// elements (ai-router, ai-agent, ai-mapping, ai-retry) can drive it without
// knowing which provider is behind the name they reference.
//
// The connector concentrates provider policy: API key, model, and the default
// response token cap. It translates the provider-agnostic core.LLM* DTOs to and
// from the Anthropic SDK types on each Complete call. Thinking is left unset so
// the model uses its adaptive default; a budget is never sent.
package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterConnector("llm-anthropic", func() core.Connector {
		return &Connector{}
	})
}

const (
	defaultModel     = "claude-opus-4-8"
	defaultMaxTokens = 4096
)

// connectorSettings is the configuration decoded from the connector's settings.
type connectorSettings struct {
	// APIKey authenticates with the Anthropic API (required). Source it from an
	// environment variable via ${ANTHROPIC_API_KEY}; it is never logged.
	APIKey string `json:"apiKey"`
	// Model is the model id (default claude-opus-4-8).
	Model string `json:"model"`
	// MaxTokens is the default response token cap (default 4096). A request may
	// override it.
	MaxTokens int `json:"maxTokens"`
	// BaseURL overrides the API endpoint (optional; for proxies or testing).
	BaseURL string `json:"baseURL"`
}

// Connector is a configured Anthropic client that AI elements call through. It
// is safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    sdk.Client
	model     string
	maxTokens int64
}

// compile-time checks that the connector is both a lifecycle connector and an
// LLM client.
var (
	_ core.Connector = (*Connector)(nil)
	_ core.LLMClient = (*Connector)(nil)
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

	opts := []option.RequestOption{option.WithAPIKey(set.APIKey)}
	if set.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(set.BaseURL))
	}
	c.client = sdk.NewClient(opts...)

	slog.Info("llm-anthropic connector started",
		"connector", config.Name,
		"model", c.model,
		"maxTokens", c.maxTokens,
	)
	return nil
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Complete runs one Messages turn, translating the request to SDK params and the
// response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	msgs, err := toMessages(req.Messages)
	if err != nil {
		return nil, err
	}
	tools, err := toTools(req.Tools)
	if err != nil {
		return nil, err
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

	message, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-anthropic complete: %w", err)
	}
	return translateResponse(message), nil
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
			blocks := make([]sdk.ContentBlockParamUnion, 0, 1+len(m.ToolCalls))
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
			out = append(out, sdk.NewAssistantMessage(blocks...))
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
// text and tool_use blocks and mapping the stop reason.
func translateResponse(message *sdk.Message) *core.LLMResponse {
	var text strings.Builder
	var calls []core.LLMToolCall
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
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
	}
	resp.Raw = core.LLMMessage{Role: core.LLMRoleAssistant, Text: resp.Text, ToolCalls: calls}
	return resp
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
