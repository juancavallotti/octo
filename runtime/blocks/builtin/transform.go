// The multi-transform leaf block applies an ordered sequence of CEL edits to the
// message in one pass. Each step either sets the body (setBody) or a variable
// (setVar/value); the edits are additive, so a later step's expression sees the
// results of the earlier ones (the activation is rebuilt before every step). It
// lets a flow compress a chain of set-payload / set-variable blocks into a single
// block while keeping every expression's surface identical to those blocks.
package builtin

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerMultiTransform() {
	core.MustRegisterBlock("multi-transform", newMultiTransform)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "multi-transform",
		Label:    "Multi Transform",
		Category: core.CategoryProcessor,
		Group:    groupData,
		Icon:     "Layers",
		Description: "Apply an ordered list of additive CEL edits in one block. Each step sets " +
			"the body (setBody) or a variable (setVar/value); the edits accumulate, so a later " +
			"step sees the results of the earlier ones. Compresses a chain of set-payload / " +
			"set-variable blocks into one.",
		Config: reflect.TypeFor[multiTransformSettings](),
	})
}

// multiTransformSettings configures the multi-transform block.
type multiTransformSettings struct {
	// Ordered list of edits. Each entry is either {setBody: <CEL>} to replace the
	// body, or {setVar: <name>, value: <CEL>} to set a variable. Applied top to
	// bottom; later expressions see body/vars produced by earlier steps.
	// Expressions see body, vars, eventID, correlationID, env, now.
	Transforms []transformStepSettings `json:"transforms" octo:"label=Transforms,type=transform-list,required"`
}

// transformStepSettings is one edit in a multi-transform. Exactly one of SetBody
// or SetVar must be set; a SetVar step also requires Value.
type transformStepSettings struct {
	// SetBody is a CEL expression whose result replaces the message body.
	SetBody string `json:"setBody"`
	// SetVar names a variable to set (readable later as vars.<SetVar>).
	SetVar string `json:"setVar"`
	// Value is the CEL expression stored under SetVar (required with SetVar).
	Value string `json:"value"`
}

// transformStep is a compiled edit: setVar names the target variable ("" means
// the step sets the body instead), and program is the expression to evaluate.
type transformStep struct {
	setVar  string
	program *expr.Program
}

// multiTransform applies its compiled steps in order, rebuilding the activation
// before each so later expressions observe the edits made by earlier ones.
type multiTransform struct {
	steps []transformStep
	env   map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newMultiTransform(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg multiTransformSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if len(cfg.Transforms) == 0 {
		return nil, errors.New("multi-transform requires at least one transform")
	}

	steps := make([]transformStep, 0, len(cfg.Transforms))
	for i, step := range cfg.Transforms {
		compiled, err := compileTransformStep(deps.Resources, i, step)
		if err != nil {
			return nil, err
		}
		steps = append(steps, compiled)
	}
	return &multiTransform{steps: steps, env: expr.EnvActivation(deps.Env)}, nil
}

// compileTransformStep validates one step (exactly one of setBody/setVar, and a
// value for setVar) and compiles its expression once, so a malformed expression
// fails at startup.
func compileTransformStep(
	res core.ResourceLoader, index int, step transformStepSettings,
) (transformStep, error) {
	hasBody := step.SetBody != ""
	hasVar := step.SetVar != ""
	switch {
	case hasBody && hasVar:
		return transformStep{}, fmt.Errorf("multi-transform step %d: set exactly one of setBody or setVar", index)
	case !hasBody && !hasVar:
		return transformStep{}, fmt.Errorf("multi-transform step %d: requires setBody or setVar", index)
	case hasBody && step.Value != "":
		return transformStep{}, fmt.Errorf("multi-transform step %d: setBody takes no value", index)
	case hasVar && step.Value == "":
		return transformStep{}, fmt.Errorf(
			"multi-transform step %d: setVar %q requires a value expression", index, step.SetVar)
	}

	source := step.SetBody
	if hasVar {
		source = step.Value
	}
	program, err := expr.CompileMessage(res, source)
	if err != nil {
		return transformStep{}, fmt.Errorf("multi-transform step %d: %w", index, err)
	}
	return transformStep{setVar: step.SetVar, program: program}, nil
}

// Process applies each step in order, rebuilding the activation before every step
// so the edits accumulate: a setBody replaces the body and a setVar stores a
// variable, both visible to the expressions that follow.
func (p *multiTransform) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	for i, step := range p.steps {
		value, err := step.program.Eval(expr.MessageActivation(msg, p.env))
		if err != nil {
			return nil, fmt.Errorf("multi-transform step %d: %w", i, err)
		}
		if step.setVar == "" {
			msg.SetBody(value)
			continue
		}
		msg.Variables.Set(step.setVar, value)
	}
	return msg, nil
}
