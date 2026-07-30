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
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/structpb"
)

// init is this module's manifest: the one place that says which CEL capabilities
// this package contributes to every message expression, in a deterministic order.
// Each extension's own registration lives beside its implementation as a
// registerX function called from here. Order is the order extensions are applied
// at compile time, so it is listed rather than left to file names.
func init() {
	registerFormDataExtension()
	registerJSONExtension()
	registerTemplateResourceExtension()
	registerStandardExtensions()
}

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
// the caller can surface at startup when the expression is malformed. Message
// expressions should use CompileMessage instead, which adds the standard message
// variables plus the registered extensions (e.g. templateResource).
func Compile(expression string, vars ...string) (*Program, error) {
	return CompileWithOptions(expression, vars)
}

// CompileWithOptions compiles expression declaring each name in vars as a
// dynamically typed variable and applying any extra environment options (custom
// functions, macros). It is the single low-level compile path; Compile and
// CompileMessage are thin wrappers over it.
func CompileWithOptions(expression string, vars []string, opts ...cel.EnvOption) (*Program, error) {
	options := make([]cel.EnvOption, 0, len(vars)+len(opts))
	for _, name := range vars {
		options = append(options, cel.Variable(name, cel.DynType))
	}
	options = append(options, opts...)

	env, err := cel.NewEnv(options...)
	if err != nil {
		return nil, fmt.Errorf("build expression env: %w", err)
	}

	checked, issues := env.Compile(expression)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile expression %q: %w", expression, issues.Err())
	}

	program, err := env.Program(checked)
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
	out, absent := unwrapOptional(out)
	if absent {
		return nil, nil
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

// unwrapOptional resolves an optional result to the value it holds, reporting
// absence separately. Functions from the regex library return optional<T> — a
// successful "no match" rather than a failure — and an optional has no JSON
// counterpart, so an absent one becomes null and a present one becomes its value.
// Anything else passes through untouched. Optionals nest (optional.of(x) over an
// x that is itself optional), so this unwraps until it reaches a plain value.
//
//nolint:ireturn // mirrors the ref.Val the caller already holds
func unwrapOptional(val ref.Val) (ref.Val, bool) {
	for {
		opt, ok := val.(*types.Optional)
		if !ok {
			return val, false
		}
		if !opt.HasValue() {
			return val, true
		}
		val = opt.GetValue()
	}
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
