// Built-in leaf blocks that mutate the message: set-payload, set-variable, and
// delete-variable. They register on the process-wide block registry so they are
// always available once the engine is linked, without importing the optional
// processors module. Each compiles its CEL expression once at build time, so a
// malformed expression fails at startup rather than per message.
package engine

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("set-payload", newSetPayload)
	core.MustRegisterBlock("set-variable", newSetVariable)
	core.MustRegisterBlock("delete-variable", newDeleteVariable)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "set-payload",
		Label:       "Set Payload",
		Category:    "processor",
		Group:       "Data",
		Icon:        "Wand2",
		Description: "Replace the message body with the result of a CEL expression.",
		Config:      reflect.TypeOf(setPayloadSettings{}),
	})
}

// setPayloadSettings configures the set-payload block.
type setPayloadSettings struct {
	// CEL expression. Sees body, vars, eventID, correlationID.
	Value string `json:"value" octo:"label=Value,type=cel,required"`
	// Store the value as a raw-content body {contentType, rawData} instead of
	// decoded JSON, so a connector (e.g. the HTTP source) serves it verbatim.
	// The value expression must then evaluate to a string.
	RawBody bool `json:"rawBody" octo:"label=Raw body,default=false"`
	// MIME type recorded on the raw body, e.g. text/html or
	// application/x-www-form-urlencoded. Required when Raw body is on.
	ContentType string `json:"contentType" octo:"label=Content type"`
}

// setPayload replaces the message body with the result of evaluating its value
// expression against the message.
type setPayload struct {
	value       *expr.Program
	rawBody     bool
	contentType string
	env         map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSetPayload(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg setPayloadSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Value == "" {
		return nil, errors.New("set-payload requires a value expression")
	}
	if cfg.RawBody && cfg.ContentType == "" {
		return nil, errors.New("set-payload requires a contentType when rawBody is set")
	}
	program, err := expr.CompileMessage(deps.Resources, cfg.Value)
	if err != nil {
		return nil, err
	}
	return &setPayload{
		value:       program,
		rawBody:     cfg.RawBody,
		contentType: cfg.ContentType,
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// Process sets the message body to the evaluated value and forwards the message.
func (p *setPayload) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	value, err := p.value.Eval(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("set-payload value: %w", err)
	}
	if p.rawBody {
		data, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("set-payload value must be a string when rawBody is set, got %T", value)
		}
		msg.SetRawBody(p.contentType, data)
		return msg, nil
	}
	msg.Body = value
	return msg, nil
}

// setVariableSettings configures the set-variable block.
type setVariableSettings struct {
	// Name is the variable to set (readable later as vars.<name>).
	Name string `json:"name"`
	// Value is a CEL expression whose result is stored under Name.
	Value string `json:"value"`
}

// setVariable stores the result of its value expression in a named variable.
type setVariable struct {
	name  string
	value *expr.Program
	env   map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSetVariable(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg setVariableSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Name == "" {
		return nil, errors.New("set-variable requires a name")
	}
	if cfg.Value == "" {
		return nil, errors.New("set-variable requires a value expression")
	}
	program, err := expr.CompileMessage(deps.Resources, cfg.Value)
	if err != nil {
		return nil, err
	}
	return &setVariable{name: cfg.Name, value: program, env: expr.EnvActivation(deps.Env)}, nil
}

// Process evaluates the value expression and stores it under the variable name.
func (p *setVariable) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	value, err := p.value.Eval(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("set-variable %q value: %w", p.name, err)
	}
	msg.Variables.Set(p.name, value)
	return msg, nil
}

// deleteVariableSettings configures the delete-variable block.
type deleteVariableSettings struct {
	// Name is the variable to remove.
	Name string `json:"name"`
}

// deleteVariable removes a named variable from the message.
type deleteVariable struct {
	name string
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newDeleteVariable(raw types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
	var cfg deleteVariableSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Name == "" {
		return nil, errors.New("delete-variable requires a name")
	}
	return &deleteVariable{name: cfg.Name}, nil
}

// Process removes the named variable (a no-op when it is absent) and forwards
// the message.
func (p *deleteVariable) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	delete(msg.Variables, p.name)
	return msg, nil
}
