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

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/expr"
	"github.com/juancavallotti/eip-go/types"
)

func init() {
	core.MustRegisterBlock("set-payload", newSetPayload)
	core.MustRegisterBlock("set-variable", newSetVariable)
	core.MustRegisterBlock("delete-variable", newDeleteVariable)
}

// setPayloadSettings configures the set-payload block.
type setPayloadSettings struct {
	// Value is a CEL expression whose result replaces the message body.
	Value string `json:"value"`
}

// setPayload replaces the message body with the result of evaluating its value
// expression against the message.
type setPayload struct {
	value *expr.Program
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSetPayload(raw types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
	var cfg setPayloadSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.Value == "" {
		return nil, errors.New("set-payload requires a value expression")
	}
	program, err := expr.Compile(cfg.Value, exprVarNames...)
	if err != nil {
		return nil, err
	}
	return &setPayload{value: program}, nil
}

// Process sets the message body to the evaluated value and forwards the message.
func (p *setPayload) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	value, err := p.value.Eval(messageActivation(msg))
	if err != nil {
		return nil, fmt.Errorf("set-payload value: %w", err)
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
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newSetVariable(raw types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
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
	program, err := expr.Compile(cfg.Value, exprVarNames...)
	if err != nil {
		return nil, err
	}
	return &setVariable{name: cfg.Name, value: program}, nil
}

// Process evaluates the value expression and stores it under the variable name.
func (p *setVariable) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	value, err := p.value.Eval(messageActivation(msg))
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
