// This file provides the "ai-mapping" block: it reshapes the message body to a
// target shape described by a prompt, optional input/output examples, and an
// optional output JSON Schema (validated).
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
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
	// CEL expression for what the model is handed, replacing the default — the
	// whole input body as a JSON document, which is what a reshaping block wants
	// and what this leaves alone when it is empty.
	//
	// It exists for the block that sends attachments. The default turn is the body,
	// so a flow that put its files there would send each of them twice: once as a
	// file the model can read, and once as a base64 string in the middle of the
	// prompt. Stating the turn is how an author says which half of the body is the
	// question and which half is the files.
	Input string `json:"input" octo:"label=Input turn,type=cel"`
	// CEL expression for the non-text content sent alongside the input: a list of
	// {mimeType, data, name} maps whose data is base64, or a data: URL.
	//
	// Requires input, and requires a connector whose model reads media; a block
	// missing either fails to build.
	Attachments string `json:"attachments" octo:"label=Attachments,type=cel"`
	// Message variable the files the model produced are written to, as a list of
	// {name, mimeType, size, data} maps whose data is base64. Empty writes nothing.
	ResponseMedia string `json:"responseMedia" octo:"label=Response media variable"`
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
	caller *llmCaller
	system string
	// input states what the model is handed, or is nil to hand it the whole body as
	// a JSON document.
	input *expr.Program
	// attachments resolves the non-text content sent with the input. Nil for a
	// block that sends none, which is every text-only mapping.
	attachments *expr.Program
	// responseMedia is the message variable the model's generated files are written
	// to, empty for a block that writes none.
	responseMedia string
	env           expr.Env
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

	block := &mapping{
		caller:        caller,
		system:        buildSystemPrompt(cfg.Prompt, inputExample, outputExample, outputSchema),
		responseMedia: strings.TrimSpace(cfg.ResponseMedia),
		env:           expr.EnvActivation(deps.Env),
		maxTokens:     cfg.MaxTokens,
		outputSchema:  outputSchema,
		schemaProgram: schemaProgram,
	}
	if err := configureMappingInput(block, cfg, deps); err != nil {
		return nil, err
	}
	return block, nil
}

// configureMappingInput compiles what the model is handed and the files sent with
// it, refusing the combinations that cannot work.
//
// Both refusals are at build time because both fail invisibly otherwise: a model
// that cannot read a file answers about one it never saw, and a body-shaped turn
// carrying its own attachments as base64 works, and bills for every byte twice.
func configureMappingInput(block *mapping, cfg mappingSettings, deps core.BlockDeps) error {
	if strings.TrimSpace(cfg.Input) != "" {
		input, err := expr.CompileMessage(deps.Resources, cfg.Input)
		if err != nil {
			return fmt.Errorf("ai-mapping block: input: %w", err)
		}
		block.input = input
	}
	if strings.TrimSpace(cfg.Attachments) == "" {
		return nil
	}
	if block.input == nil {
		return errors.New(
			"ai-mapping block: attachments requires input, because the default turn is the whole " +
				"body as JSON and would send any attachment on the body a second time as base64")
	}
	if accepted := block.caller.acceptsMedia(); len(accepted) == 0 {
		return fmt.Errorf(
			"ai-mapping block: connector %q reads only text, so it cannot be sent files", cfg.Connector)
	}
	attachments, err := expr.CompileMessage(deps.Resources, cfg.Attachments)
	if err != nil {
		return fmt.Errorf("ai-mapping block: attachments: %w", err)
	}
	block.attachments = attachments
	return nil
}

// inputTurn is what the model is handed: the block's stated turn, or the whole
// body as a JSON document when it states none.
func (m *mapping) inputTurn(msg *types.Message, activation map[string]any) (string, error) {
	if m.input == nil {
		body, err := msg.BodyJSON()
		if err != nil {
			return "", fmt.Errorf("ai-mapping: encode input body: %w", err)
		}
		return string(body), nil
	}
	turn, err := m.input.EvalString(activation)
	if err != nil {
		return "", fmt.Errorf("ai-mapping: input: %w", err)
	}
	return turn, nil
}

// writeMedia puts whatever files the model produced on the block's response media
// variable, and does nothing for a block that names none.
//
// It replaces rather than accumulates, unlike the agent's: ai-mapping calls the
// model once per message, so there is no earlier turn to have written anything.
func (m *mapping) writeMedia(msg *types.Message, resp *core.LLMResponse) {
	if m.responseMedia == "" || resp == nil || len(resp.Media) == 0 {
		return
	}
	out := make([]any, 0, len(resp.Media))
	for _, file := range resp.Media {
		out = append(out, mediaFields(file))
	}
	msg.Variables.Set(m.responseMedia, out)
}

// Process sends the current body to the LLM, parses the JSON response, validates
// it against the output schema when one is configured, and replaces the body. A
// validation failure returns an error so the message flows to a recovery path
// (ai-retry, handle-errors, or the flow-level error path).
func (m *mapping) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, m.env)
	input, err := m.inputTurn(msg, activation)
	if err != nil {
		return nil, err
	}
	files, err := evalAttachments(m.attachments, activation)
	if err != nil {
		return nil, fmt.Errorf("ai-mapping: attachments: %w", err)
	}

	// No iteration: ai-mapping calls the model once per message, so there is no
	// loop for a record to number.
	resp, err := m.caller.complete(ctx, msg, core.LLMRequest{
		System:    m.system,
		Messages:  []core.LLMMessage{{Role: core.LLMRoleUser, Text: input, Attachments: files}},
		MaxTokens: m.maxTokens,
	}, turnLabel{})
	if err != nil {
		return nil, fmt.Errorf("ai-mapping: %w", err)
	}
	m.writeMedia(msg, resp)

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
