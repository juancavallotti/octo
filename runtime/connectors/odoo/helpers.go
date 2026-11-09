// This file holds what the odoo blocks share: binding a block to its connector,
// compiling and evaluating their CEL settings into the shapes a Request takes,
// and delivering a result or tolerating a failure.
package odoo

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// resolveConnector binds a block to its odoo connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("odoo connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not an odoo connector", name)
	}
	return conn, nil
}

// compileOptional compiles a message CEL expression, returning a nil program for
// an empty source so callers can treat "unset" as "skip".
func compileOptional(res core.ResourceLoader, block, field, src string) (*expr.Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	program, err := expr.CompileMessage(res, src)
	if err != nil {
		return nil, fmt.Errorf("%s: compile %s: %w", block, field, err)
	}
	return program, nil
}

// compileRequired compiles a required message CEL expression, erroring with a
// block- and field-labelled message when it is empty or malformed.
func compileRequired(res core.ResourceLoader, block, field, src string) (*expr.Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("%s requires a %q expression", block, field)
	}
	return compileOptional(res, block, field, src)
}

// target is the part every odoo block shares: the connector, the model it
// calls, the optional call context, and where the result goes.
type target struct {
	block       string
	conn        *Connector
	model       *expr.Program
	context     *expr.Program
	resultVar   string
	failOnError bool
	env         expr.Env
}

func newTarget(
	block string, deps core.BlockDeps, connector, model, callContext, resultVar string, failOnError *bool,
) (target, error) {
	conn, err := resolveConnector(connector, deps)
	if err != nil {
		return target{}, fmt.Errorf("%s: %w", block, err)
	}
	modelProgram, err := compileRequired(deps.Resources, block, "model", model)
	if err != nil {
		return target{}, err
	}
	contextProgram, err := compileOptional(deps.Resources, block, "context", callContext)
	if err != nil {
		return target{}, err
	}
	return target{
		block:       block,
		conn:        conn,
		model:       modelProgram,
		context:     contextProgram,
		resultVar:   resultVar,
		failOnError: failOnError == nil || *failOnError,
		env:         expr.EnvActivation(deps.Env),
	}, nil
}

// request evaluates the model and context for one message into a Request for
// method; the caller fills in ids and params.
func (t target) request(activation map[string]any, method string) (Request, error) {
	model, err := t.model.EvalString(activation)
	if err != nil {
		return Request{}, fmt.Errorf("%s model: %w", t.block, err)
	}
	callContext, err := evalMap(t.context, activation)
	if err != nil {
		return Request{}, fmt.Errorf("%s context: %w", t.block, err)
	}
	return Request{Model: model, Method: method, Context: callContext}, nil
}

// run performs req and delivers its result: into resultVar when one is named,
// otherwise as the body. A failed call aborts the flow unless failOnError is
// off, in which case the message passes through unchanged.
func (t target) run(ctx context.Context, msg *types.Message, req Request) (*types.Message, error) {
	result, err := t.conn.Call(ctx, req)
	if err != nil {
		if t.failOnError {
			return nil, fmt.Errorf("%s: %w", t.block, err)
		}
		return msg, nil
	}
	if t.resultVar != "" {
		msg.Variables.Set(t.resultVar, result)
	} else {
		msg.SetBody(result)
	}
	return msg, nil
}

// evalMap evaluates an optional expression that must produce an object; a nil
// program or a null result is a nil map.
func evalMap(program *expr.Program, activation map[string]any) (map[string]any, error) {
	if program == nil {
		return nil, nil
	}
	value, err := program.Eval(activation)
	if err != nil || value == nil {
		return nil, err
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("want an object, got %T", value)
	}
	return m, nil
}

// evalIDs evaluates a record-id expression: one id or a list of them. The
// result is never nil, so a block that names ids always calls a recordset, an
// empty list included.
func evalIDs(program *expr.Program, activation map[string]any) ([]int64, error) {
	value, err := program.Eval(activation)
	if err != nil {
		return nil, err
	}
	list, isList := value.([]any)
	if !isList {
		list = []any{value}
	}
	ids := make([]int64, 0, len(list))
	for _, item := range list {
		id, err := toID(item)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// toID converts one evaluated value to a record id. CEL numbers arrive as
// float64; a many2one value ([id, "Name"]) contributes its id, so a field read
// from one record can name another without unpacking it first.
func toID(value any) (int64, error) {
	switch v := value.(type) {
	case float64:
		if v != math.Trunc(v) || v <= 0 {
			return 0, fmt.Errorf("record id %v is not a positive integer", v)
		}
		return int64(v), nil
	case []any:
		if len(v) == 2 {
			return toID(v[0])
		}
	}
	return 0, fmt.Errorf("record id must be a number, got %v", value)
}
