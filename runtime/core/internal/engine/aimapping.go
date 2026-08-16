// This file provides the "ai-mapping" block: it reshapes the message body to a
// target shape described by a prompt, optional input/output examples, and an
// optional output JSON Schema (validated).
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// blockTypeAIMapping is the registry name and YAML type of the ai-mapping block.
// Unlike the ai-* composites in flow.go, it is dispatched by the block registry,
// not by the builder.
const blockTypeAIMapping = "ai-mapping"

func registerAIMapping() {
	core.MustRegisterBlock(blockTypeAIMapping, newAIMapping)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockTypeAIMapping,
		Label:    "AI Mapping",
		Category: core.CategoryProcessor,
		Group:    groupAILLM,
		Icon:     "Sparkles",
		Description: "Reshape the message body to a target shape described by a prompt; validates " +
			"against an optional output schema and errors on failure.",
		Config: reflect.TypeFor[mappingSettings](),
	})
}

// mappingSettings is the ai-mapping block's typed configuration. The schema and
// example fields are JSON documents; in YAML they are written either as an inline
// JSON string (a block scalar) or as a native map — both are normalized to raw
// JSON at build time.
type mappingSettings struct {
	// Name of the LLM connector to use.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// Instruction describing how to reshape the body.
	Prompt string `json:"prompt" octo:"label=Prompt,required"`
	// Example input payload that guides recognition (JSON).
	InputExample json.RawMessage `json:"inputExample" octo:"label=Input example,type=string"`
	// Example output payload that shapes the result (JSON).
	OutputExample json.RawMessage `json:"outputExample" octo:"label=Output example,type=string"`
	// JSON Schema the result is validated against; a failure errors the block.
	OutputSchema json.RawMessage `json:"outputSchema" octo:"label=Output schema,type=string"`
	// Response token cap for this call (0 = connector default).
	MaxTokens int `json:"maxTokens" octo:"label=Max tokens"`
}

// mapping reshapes the body via the LLM, optionally validating the result.
type mapping struct {
	caller        *llmCaller
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

	caller, err := resolveLLM(blockTypeAIMapping, cfg.Connector, deps)
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
		caller:        caller,
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

	// No iteration: ai-mapping calls the model once per message, so there is no
	// loop for a record to number.
	resp, err := m.caller.complete(ctx, msg, core.LLMRequest{
		System:    m.system,
		Messages:  []core.LLMMessage{{Role: core.LLMRoleUser, Text: string(input)}},
		MaxTokens: m.maxTokens,
	}, turnLabel{})
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

	msg.SetBody(decoded)
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
