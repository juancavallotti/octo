// Package gemini provides the "llm-gemini" connector: a configured Google Gemini
// client that satisfies core.LLMClient so the AI flow elements can drive it
// interchangeably with the other providers. It translates the provider-agnostic
// core.LLM* DTOs to and from the genai SDK types on each Complete call.
//
// Gemini has no explicit tool-call IDs and returns function calls with a STOP
// finish reason, so this connector synthesizes the agnostic ToolCallID from the
// function name (correlating tool results by name) and reports a tool-use stop
// reason whenever the response carries function calls.
package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"google.golang.org/genai"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterConnector("llm-gemini", func() core.Connector {
		return &Connector{}
	})
}

const defaultModel = "gemini-3.5-flash"

// connectorSettings is the configuration decoded from the connector's settings.
type connectorSettings struct {
	// APIKey authenticates with the Gemini API (required). Source it from an
	// environment variable via ${GEMINI_API_KEY}; it is never logged.
	APIKey string `json:"apiKey"`
	// Model is the model id (default gemini-3.5-flash).
	Model string `json:"model"`
	// MaxTokens is the default response token cap (0 = the model default). A
	// request may override it.
	MaxTokens int `json:"maxTokens"`
	// BaseURL overrides the API endpoint (optional; for proxies or testing).
	BaseURL string `json:"baseURL"`
}

// Connector is a configured Gemini client that AI elements call through. It is
// safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    *genai.Client
	model     string
	maxTokens int
}

var (
	_ core.Connector = (*Connector)(nil)
	_ core.LLMClient = (*Connector)(nil)
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
	)
	return nil
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Complete runs one GenerateContent turn, translating the request to SDK types
// and the response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	contents, err := toContents(req.Messages)
	if err != nil {
		return nil, err
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
	if tools, toolErr := toTools(req.Tools); toolErr != nil {
		return nil, toolErr
	} else if tools != nil {
		cfg.Tools = tools
		if tc, ok := toToolConfig(req.ToolChoice); ok {
			cfg.ToolConfig = tc
		}
	}

	resp, err := c.client.Models.GenerateContent(ctx, c.model, contents, cfg)
	if err != nil {
		return nil, fmt.Errorf("llm-gemini complete: %w", err)
	}
	return translateResponse(resp), nil
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
	if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
		return &core.LLMResponse{StopReason: core.LLMStopRefusal, Raw: core.LLMMessage{Role: core.LLMRoleAssistant}}
	}
	cand := resp.Candidates[0]

	var text strings.Builder
	var calls []core.LLMToolCall
	var sig []byte // most recent thought signature; may sit on a thought part before the call
	for _, part := range cand.Content.Parts {
		if len(part.ThoughtSignature) > 0 {
			sig = part.ThoughtSignature
		}
		if part.Text != "" {
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
	}
	out.Raw = core.LLMMessage{Role: core.LLMRoleAssistant, Text: out.Text, ToolCalls: calls}
	return out
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
