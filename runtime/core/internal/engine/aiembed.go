// This file provides the "ai-embed" block: it evaluates a CEL expression against
// the message, embeds the result via an LLM connector's embeddings capability,
// and stores the vector(s) in a variable. Unlike ai-mapping, it never touches the
// message body — a vector is an auxiliary value alongside the original body, not
// a replacement for it.
package engine

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// blockTypeAIEmbed is the registry name and YAML type of the ai-embed block.
const blockTypeAIEmbed = "ai-embed"

func registerAIEmbed() {
	core.MustRegisterBlock(blockTypeAIEmbed, newAIEmbed)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     blockTypeAIEmbed,
		Label:    "AI Embed",
		Category: core.CategoryProcessor,
		Group:    groupAILLM,
		Icon:     "ScatterChart",
		Description: "Turn text into one or more embedding vectors via an OpenAI or Gemini connector, " +
			"stored in a variable. A connector whose provider serves no embeddings API is rejected.",
		Config: reflect.TypeFor[embedSettings](),
	})
}

// defaultEmbedResultVar names the variable an embedding result is stored in.
const defaultEmbedResultVar = "embedding"

// embedSettings is the ai-embed block's typed configuration.
type embedSettings struct {
	// Name of the LLM connector to use. Must support embeddings (llm-openai or
	// llm-gemini); a connector whose provider serves no embeddings API is rejected
	// at build time.
	Connector string `json:"connector" octo:"label=Connector,required,ref=connector-category:llm"`
	// CEL expression evaluated to either a string (embedded singly) or a list of
	// strings (embedded as one batch call). The result shape decides whether
	// resultVar is written as a single vector or a list of vectors.
	Text string `json:"text" octo:"label=Text,required,type=cel"`
	// Provider-specific embedding model id (e.g. text-embedding-3-small,
	// gemini-embedding-001).
	Model string `json:"model" octo:"label=Model,required"`
	// Requests a shortened output embedding when the model supports it (0 = model
	// default).
	Dimensions int `json:"dimensions" octo:"label=Dimensions"`
	// Variable the embedding result is stored in.
	ResultVar string `json:"resultVar" octo:"label=Result variable,default=embedding"`
}

// embed evaluates the text expression, embeds it, and writes the result to a
// variable.
type embed struct {
	caller     *embedCaller
	text       *expr.Program
	model      string
	dimensions int
	resultVar  string
	env        map[string]any
}

// newAIEmbed builds the block, resolving the embedding connector and compiling
// the text expression once so a bad reference or expression fails at startup
// rather than at runtime.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newAIEmbed(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg embedSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("ai-embed block: model is required")
	}

	caller, err := resolveEmbed(cfg.Connector, deps)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(cfg.Text) == "" {
		return nil, fmt.Errorf("ai-embed block: text is required")
	}
	text, err := expr.CompileMessage(deps.Resources, cfg.Text)
	if err != nil {
		return nil, fmt.Errorf("ai-embed block: compile text: %w", err)
	}

	resultVar := cfg.ResultVar
	if resultVar == "" {
		resultVar = defaultEmbedResultVar
	}

	return &embed{
		caller:     caller,
		text:       text,
		model:      cfg.Model,
		dimensions: cfg.Dimensions,
		resultVar:  resultVar,
		env:        expr.EnvActivation(deps.Env),
	}, nil
}

// Process evaluates the text expression, embeds the resulting text(s), and
// stores the vector (or list of vectors, for a batch) in the result variable.
func (e *embed) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, e.env)
	value, err := e.text.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("ai-embed: text: %w", err)
	}

	input, batch, err := toEmbedInputs(value)
	if err != nil {
		return nil, fmt.Errorf("ai-embed: %w", err)
	}

	resp, err := e.caller.embed(ctx, msg, core.EmbedRequest{
		Model:      e.model,
		Input:      input,
		Dimensions: e.dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("ai-embed: %w", err)
	}

	if batch {
		msg.Variables.Set(e.resultVar, resp.Vectors)
	} else {
		msg.Variables.Set(e.resultVar, resp.Vectors[0])
	}
	return msg, nil
}

// toEmbedInputs normalizes the evaluated text expression into a batch of inputs.
// A string result embeds singly (batch=false); a non-empty list of strings
// embeds as one batch call (batch=true). The shape drives whether Process writes
// resultVar as a single vector or a list of vectors.
func toEmbedInputs(value any) (input []string, batch bool, err error) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil, false, fmt.Errorf("text evaluated to an empty string")
		}
		return []string{v}, false, nil
	case []any:
		if len(v) == 0 {
			return nil, false, fmt.Errorf("text evaluated to an empty list")
		}
		texts := make([]string, len(v))
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, false, fmt.Errorf("text[%d] is not a string (got %T)", i, item)
			}
			texts[i] = s
		}
		return texts, true, nil
	default:
		return nil, false, fmt.Errorf("text must evaluate to a string or a list of strings (got %T)", value)
	}
}

// resolveEmbed binds a block to an LLM provider connector by name, asserting the
// core.EmbedClient interface. Only llm-openai and llm-gemini implement it — an
// llm-anthropic or llm-openrouter connector (or any other non-embedding one)
// fails this assertion, which is how ai-embed rejects them at build time rather
// than special-casing provider names.
//
// It returns the caller rather than the client for the same reason resolveLLM
// does: an embedding call is billed, and the record for it should not depend on a
// block author remembering to write one.
func resolveEmbed(name string, deps core.BlockDeps) (*embedCaller, error) {
	if name == "" {
		return nil, fmt.Errorf("ai-embed block: connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("ai-embed block: connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("ai-embed block: connector %q is not configured", name)
	}
	client, ok := connector.(core.EmbedClient)
	if !ok {
		return nil, fmt.Errorf(
			"ai-embed block: connector %q does not support embeddings; "+
				"use an llm-openai or llm-gemini connector", name)
	}
	return &embedCaller{client: client, who: newIdentity(blockTypeAIEmbed, name, providerOf(connector), deps)}, nil
}
