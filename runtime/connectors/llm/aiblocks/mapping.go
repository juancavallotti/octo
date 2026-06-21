// Package aiblocks provides leaf processor blocks powered by an LLM connector.
// The composite AI elements (ai-router, ai-agent, ai-retry) are built by the
// flow engine; the leaf elements that fit the block registry live here.
//
// The only leaf today is "ai-mapping": it reshapes the message body to a target
// shape described by a prompt, optional input/output examples, and an optional
// output JSON Schema (validated). Blocks here bind to an LLM provider by name and
// type-assert to core.LLMClient — the shared interface — so they work with any
// configured provider (llm-anthropic, llm-openai, llm-gemini).
package aiblocks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterBlock("ai-mapping", newAIMapping)
}

// mappingSettings is the ai-mapping block's typed configuration. The schema and
// example fields are JSON documents; in YAML they are written either as an inline
// JSON string (a block scalar) or as a native map — both are normalized to raw
// JSON at build time.
type mappingSettings struct {
	// Connector names the LLM connector to call through (required).
	Connector string `json:"connector"`
	// Prompt describes the transformation (required).
	Prompt string `json:"prompt"`
	// InputExample is an example of the input the block receives (optional).
	InputExample json.RawMessage `json:"inputExample"`
	// OutputExample is an example of the desired output (optional).
	OutputExample json.RawMessage `json:"outputExample"`
	// OutputSchema is a JSON Schema the output is validated against (optional).
	OutputSchema json.RawMessage `json:"outputSchema"`
	// MaxTokens overrides the connector's default response cap (optional).
	MaxTokens int `json:"maxTokens"`
}

// mapping reshapes the body via the LLM, optionally validating the result.
type mapping struct {
	client        core.LLMClient
	system        string
	maxTokens     int
	outputSchema  json.RawMessage
	schemaProgram *jsonschema.Schema
}

// newAIMapping builds the block, resolving the LLM connector and compiling the
// output schema once so a bad reference or schema fails at startup rather than at
// runtime. The system prompt is assembled once, too.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newAIMapping(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg mappingSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return nil, fmt.Errorf("ai-mapping block: prompt is required")
	}

	client, err := resolveLLM(cfg.Connector, deps)
	if err != nil {
		return nil, err
	}

	inputExample, err := asJSONDocument(cfg.InputExample)
	if err != nil {
		return nil, fmt.Errorf("ai-mapping block: inputExample: %w", err)
	}
	outputExample, err := asJSONDocument(cfg.OutputExample)
	if err != nil {
		return nil, fmt.Errorf("ai-mapping block: outputExample: %w", err)
	}
	outputSchema, err := asJSONDocument(cfg.OutputSchema)
	if err != nil {
		return nil, fmt.Errorf("ai-mapping block: outputSchema: %w", err)
	}

	var schemaProgram *jsonschema.Schema
	if len(outputSchema) > 0 {
		schemaProgram, err = jsonschema.CompileString("ai-mapping-output.json", string(outputSchema))
		if err != nil {
			return nil, fmt.Errorf("ai-mapping block: compile outputSchema: %w", err)
		}
	}

	return &mapping{
		client:        client,
		system:        buildSystemPrompt(cfg.Prompt, inputExample, outputExample, outputSchema),
		maxTokens:     cfg.MaxTokens,
		outputSchema:  outputSchema,
		schemaProgram: schemaProgram,
	}, nil
}

// Process sends the current body to the LLM, parses the JSON response, validates
// it against the output schema when one is configured, and replaces the body. A
// validation failure returns an error so the message flows to a recovery path
// (ai-retry, handle-errors, or the flow-level error path).
func (m *mapping) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	input, err := msg.BodyJSON()
	if err != nil {
		return nil, fmt.Errorf("ai-mapping: encode input body: %w", err)
	}

	resp, err := m.client.Complete(ctx, core.LLMRequest{
		System:    m.system,
		Messages:  []core.LLMMessage{{Role: core.LLMRoleUser, Text: string(input)}},
		MaxTokens: m.maxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("ai-mapping: %w", err)
	}

	output := stripJSONFence(resp.Text)
	var decoded any
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		return nil, fmt.Errorf("ai-mapping: response is not valid JSON: %w", err)
	}

	if m.schemaProgram != nil {
		if err := m.schemaProgram.Validate(decoded); err != nil {
			return nil, fmt.Errorf("ai-mapping: output failed schema validation: %w", err)
		}
	}

	msg.Body = decoded
	if len(m.outputSchema) > 0 {
		msg.BodySchema = m.outputSchema
	}
	return msg, nil
}

// buildSystemPrompt assembles the fixed transform instruction, the user's prompt,
// and whichever input/output contracts were supplied.
func buildSystemPrompt(prompt string, inputExample, outputExample, outputSchema json.RawMessage) string {
	var b strings.Builder
	b.WriteString("You transform a JSON input document into a JSON output document.\n")
	b.WriteString("Respond with ONLY the output JSON: no prose, no explanation, no markdown code fences.\n\n")
	b.WriteString(strings.TrimSpace(prompt))
	if len(inputExample) > 0 {
		b.WriteString("\n\nExample input:\n")
		b.Write(inputExample)
	}
	if len(outputExample) > 0 {
		b.WriteString("\n\nExample output:\n")
		b.Write(outputExample)
	}
	if len(outputSchema) > 0 {
		b.WriteString("\n\nThe output must conform to this JSON Schema:\n")
		b.Write(outputSchema)
	}
	return b.String()
}

// stripJSONFence removes a surrounding ```json ... ``` (or bare ``` ... ```)
// markdown fence if the model wrapped its answer in one despite instructions.
func stripJSONFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimPrefix(s, "json")
	s = strings.TrimPrefix(s, "JSON")
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// asJSONDocument normalizes a settings value that may be either a JSON document
// (a native YAML map) or a string containing JSON into raw JSON bytes. It returns
// nil for an empty value.
func asJSONDocument(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("decode JSON string: %w", err)
		}
		return json.RawMessage(strings.TrimSpace(s)), nil
	}
	return raw, nil
}

// resolveLLM binds a block to an LLM provider connector by name, asserting the
// shared core.LLMClient interface rather than a concrete connector type so any
// provider satisfies it. Returning the interface is intentional: it is what lets
// a block bind to any provider connector.
//
//nolint:ireturn // the shared interface is the binding mechanism, by design
func resolveLLM(name string, deps core.BlockDeps) (core.LLMClient, error) {
	if name == "" {
		return nil, fmt.Errorf("ai-mapping block: connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("ai-mapping block: connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("ai-mapping block: connector %q is not configured", name)
	}
	client, ok := connector.(core.LLMClient)
	if !ok {
		return nil, fmt.Errorf("ai-mapping block: connector %q is not an LLM provider", name)
	}
	return client, nil
}
