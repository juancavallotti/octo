package expr

import (
	"encoding/json"
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/structpb"
	"gopkg.in/yaml.v3"
)

// toYAMLFuncName renders any value as a YAML document; fromYAMLFuncName parses a
// YAML document back into a decoded value. Together they bridge between a
// raw-content string payload (body.rawData) and a structured body — e.g.
// set-payload: fromYaml(body.rawData) turns a YAML file that was read as raw text
// into a normal JSON-native body.
const (
	toYAMLFuncName   = "toYaml"
	fromYAMLFuncName = "fromYaml"
)

func registerYAMLExtension() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return yamlOptions() })
}

// yamlOptions declares the toYaml/fromYaml message functions. Like toJson/fromJson
// they act purely on their argument, so plain unary functions suffice.
func yamlOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(toYAMLFuncName,
			cel.Overload(toYAMLFuncName+"_dyn_string",
				[]*cel.Type{cel.DynType}, cel.StringType,
				cel.UnaryBinding(toYAMLBinding))),
		cel.Function(fromYAMLFuncName,
			cel.Overload(fromYAMLFuncName+"_string_dyn",
				[]*cel.Type{cel.StringType}, cel.DynType,
				cel.UnaryBinding(fromYAMLBinding))),
	}
}

// toYAMLBinding renders the value as a YAML document. It first normalizes the CEL
// value to a JSON-native Go value (the same structpb bridge Eval uses) so nested
// maps render as YAML mappings rather than as cel-go's map[any]any.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func toYAMLBinding(val ref.Val) ref.Val {
	native, err := val.ConvertToNative(structValueType)
	if err != nil {
		return types.NewErr("toYaml: %v", err)
	}
	sv, ok := native.(*structpb.Value)
	if !ok {
		return types.NewErr("toYaml: unexpected value type %T", native)
	}
	raw, err := yaml.Marshal(sv.AsInterface())
	if err != nil {
		return types.NewErr("toYaml: %v", err)
	}
	return types.String(raw)
}

// fromYAMLBinding parses a YAML document into a decoded value and adapts it back
// to a CEL value.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func fromYAMLBinding(val ref.Val) ref.Val {
	s, ok := val.Value().(string)
	if !ok {
		return types.NewErr("fromYaml: argument must be a string, got %v", val.Type())
	}
	var decoded any
	if err := yaml.Unmarshal([]byte(s), &decoded); err != nil {
		return types.NewErr("fromYaml: %v", err)
	}
	native, err := jsonNative(decoded)
	if err != nil {
		return types.NewErr("fromYaml: %v", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(native)
}

// jsonNative re-decodes a YAML-decoded value through JSON so it holds only
// JSON-native shapes. YAML is the wider language: it decodes integers as int and
// timestamps as time.Time, neither of which a message body may carry, and octo's
// contract is that a body is decoded JSON (numbers are float64). The round-trip
// normalizes what JSON can represent and fails loudly on what it cannot — a
// non-string mapping key, say — rather than leaking a foreign type into a body.
func jsonNative(decoded any) (any, error) {
	raw, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("value is not representable as JSON: %w", err)
	}
	var native any
	if err := json.Unmarshal(raw, &native); err != nil {
		return nil, fmt.Errorf("decode normalized value: %w", err)
	}
	return native, nil
}
