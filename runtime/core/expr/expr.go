// Package expr is the runtime's expression engine. It wraps Common Expression
// Language (CEL) so processors and sources compile and evaluate expressions the
// same way without touching cel-go internals.
//
// Expressions are compiled once (at flow-build time, so a bad expression fails
// fast at startup) and evaluated many times against an activation: a map of the
// variable names declared at compile time to their per-evaluation values.
package expr

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/google/cel-go/cel"
	"google.golang.org/protobuf/types/known/structpb"
)

// structValueType is the conversion target that bridges any CEL result to a
// JSON-native Go value, keeping results consistent with the runtime's JSON-only
// message contract (objects become map[string]any, numbers float64, and so on).
var structValueType = reflect.TypeOf(&structpb.Value{})

// Program is a compiled, reusable expression. It is safe for concurrent
// evaluation, matching the MessageProcessor thread-safety contract.
type Program struct {
	program cel.Program
}

// Compile checks and compiles expression, declaring each name in vars as a
// dynamically typed variable available to the expression. It returns an error
// the caller can surface at startup when the expression is malformed.
func Compile(expression string, vars ...string) (*Program, error) {
	options := make([]cel.EnvOption, 0, len(vars))
	for _, name := range vars {
		options = append(options, cel.Variable(name, cel.DynType))
	}

	env, err := cel.NewEnv(options...)
	if err != nil {
		return nil, fmt.Errorf("build expression env: %w", err)
	}

	ast, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile expression %q: %w", expression, issues.Err())
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("plan expression %q: %w", expression, err)
	}

	return &Program{program: program}, nil
}

// Eval evaluates the program against activation and returns the result as a
// JSON-native Go value (string, float64, bool, nil, map[string]any, []any). Keys
// absent from activation are simply unbound; referencing them yields an
// evaluation error.
func (p *Program) Eval(activation map[string]any) (any, error) {
	out, _, err := p.program.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("evaluate expression: %w", err)
	}

	native, err := out.ConvertToNative(structValueType)
	if err != nil {
		return nil, fmt.Errorf("convert expression result: %w", err)
	}
	value, ok := native.(*structpb.Value)
	if !ok {
		return nil, fmt.Errorf("expression result has unexpected type %T", native)
	}
	return value.AsInterface(), nil
}

// EvalString evaluates the program and renders the result as a string. A string
// result is returned verbatim; any other value is rendered as compact JSON so
// objects and arrays log readably.
func (p *Program) EvalString(activation map[string]any) (string, error) {
	value, err := p.Eval(activation)
	if err != nil {
		return "", err
	}
	if s, ok := value.(string); ok {
		return s, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value), nil //nolint:nilerr // fall back to Go formatting
	}
	return string(raw), nil
}
