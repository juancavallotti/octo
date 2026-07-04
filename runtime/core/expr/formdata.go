package expr

import (
	"fmt"
	"net/url"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/structpb"
)

// toFormDataFuncName encodes an object as an application/x-www-form-urlencoded
// string; fromFormDataFuncName parses one back into an object. They bridge a raw
// form payload (body.rawData) and a structured body — e.g.
// set-payload: fromFormData(body.rawData) parses a raw form POST, and
// toFormData(body) builds a form body to send or serve.
const (
	toFormDataFuncName   = "toFormData"
	fromFormDataFuncName = "fromFormData"
)

func init() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return formDataOptions() })
}

// formDataOptions declares the toFormData/fromFormData message functions. Like
// toJson/fromJson they act purely on their argument, so plain unary functions
// suffice. Only application/x-www-form-urlencoded is handled (not multipart).
func formDataOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(toFormDataFuncName,
			cel.Overload(toFormDataFuncName+"_dyn_string",
				[]*cel.Type{cel.DynType}, cel.StringType,
				cel.UnaryBinding(toFormDataBinding))),
		cel.Function(fromFormDataFuncName,
			cel.Overload(fromFormDataFuncName+"_string_dyn",
				[]*cel.Type{cel.StringType}, cel.DynType,
				cel.UnaryBinding(fromFormDataBinding))),
	}
}

// toFormDataBinding encodes an object as a urlencoded form string. Each field is
// a scalar; an array field emits a repeated key. Encode sorts keys, so the output
// is deterministic.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func toFormDataBinding(val ref.Val) ref.Val {
	native, err := val.ConvertToNative(structValueType)
	if err != nil {
		return types.NewErr("toFormData: %v", err)
	}
	sv, ok := native.(*structpb.Value)
	if !ok {
		return types.NewErr("toFormData: unexpected value type %T", native)
	}
	fields, ok := sv.AsInterface().(map[string]any)
	if !ok {
		return types.NewErr("toFormData: value must be an object")
	}
	values := url.Values{}
	for key, v := range fields {
		if list, ok := v.([]any); ok {
			for _, el := range list {
				values.Add(key, formScalar(el))
			}
			continue
		}
		values.Set(key, formScalar(v))
	}
	return types.String(values.Encode())
}

// fromFormDataBinding parses a urlencoded form string into an object: a single
// value becomes a string, repeated keys become a list of strings.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func fromFormDataBinding(val ref.Val) ref.Val {
	s, ok := val.Value().(string)
	if !ok {
		return types.NewErr("fromFormData: argument must be a string, got %v", val.Type())
	}
	parsed, err := url.ParseQuery(s)
	if err != nil {
		return types.NewErr("fromFormData: %v", err)
	}
	out := make(map[string]any, len(parsed))
	for key, vs := range parsed {
		if len(vs) == 1 {
			out[key] = vs[0]
			continue
		}
		list := make([]any, len(vs))
		for i, v := range vs {
			list[i] = v
		}
		out[key] = list
	}
	return types.DefaultTypeAdapter.NativeToValue(out)
}

// formScalar renders a JSON-native scalar as its form field value. A nil (JSON
// null) becomes the empty string.
func formScalar(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
