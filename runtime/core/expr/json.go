package expr

import (
	"encoding/json"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/structpb"
)

// toJSONFuncName marshals any value to a compact JSON string; fromJSONFuncName
// parses a JSON string back into a decoded value. Together they bridge between a
// raw-content string payload (body.rawData) and a structured body — e.g.
// set-payload: fromJson(body.rawData) reverts a captured raw JSON body.
const (
	toJSONFuncName   = "toJson"
	fromJSONFuncName = "fromJson"
)

func init() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return jsonOptions() })
}

// jsonOptions declares the toJson/fromJson message functions. They operate purely
// on their argument (no activation access), so plain unary functions suffice.
func jsonOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(toJSONFuncName,
			cel.Overload(toJSONFuncName+"_dyn_string",
				[]*cel.Type{cel.DynType}, cel.StringType,
				cel.UnaryBinding(toJSONBinding))),
		cel.Function(fromJSONFuncName,
			cel.Overload(fromJSONFuncName+"_string_dyn",
				[]*cel.Type{cel.StringType}, cel.DynType,
				cel.UnaryBinding(fromJSONBinding))),
	}
}

// toJSONBinding marshals the value to a JSON string. It first normalizes the CEL
// value to a JSON-native Go value (the same structpb bridge Eval uses) so nested
// maps marshal cleanly rather than as cel-go's map[any]any.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func toJSONBinding(val ref.Val) ref.Val {
	native, err := val.ConvertToNative(structValueType)
	if err != nil {
		return types.NewErr("toJson: %v", err)
	}
	sv, ok := native.(*structpb.Value)
	if !ok {
		return types.NewErr("toJson: unexpected value type %T", native)
	}
	raw, err := json.Marshal(sv.AsInterface())
	if err != nil {
		return types.NewErr("toJson: %v", err)
	}
	return types.String(raw)
}

// fromJSONBinding parses a JSON string into a decoded value (map[string]any,
// []any, float64, string, bool, nil) and adapts it back to a CEL value.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func fromJSONBinding(val ref.Val) ref.Val {
	s, ok := val.Value().(string)
	if !ok {
		return types.NewErr("fromJson: argument must be a string, got %v", val.Type())
	}
	var decoded any
	if err := json.Unmarshal([]byte(s), &decoded); err != nil {
		return types.NewErr("fromJson: %v", err)
	}
	return types.DefaultTypeAdapter.NativeToValue(decoded)
}
