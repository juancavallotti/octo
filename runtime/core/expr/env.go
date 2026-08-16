package expr

import (
	"fmt"
	"sort"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/juancavallotti/octo/runtime/core/dotenv"
)

// toEnvFuncName renders a flat object as .env file content; fromEnvFuncName parses
// .env content back into an object of string values. Together they bridge between
// a raw-content string payload (body.rawData) and a structured body — e.g.
// set-payload: fromEnv(body.rawData) turns an env file that was read as raw text
// into a normal body, and toEnv(body) builds one to write back out.
const (
	toEnvFuncName   = "toEnv"
	fromEnvFuncName = "fromEnv"
)

func registerEnvExtension() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return envOptions() })
}

// envOptions declares the toEnv/fromEnv message functions. Like toJson/fromJson
// they act purely on their argument, so plain unary functions suffice.
func envOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(toEnvFuncName,
			cel.Overload(toEnvFuncName+"_dyn_string",
				[]*cel.Type{cel.DynType}, cel.StringType,
				cel.UnaryBinding(toEnvBinding))),
		cel.Function(fromEnvFuncName,
			cel.Overload(fromEnvFuncName+"_string_dyn",
				[]*cel.Type{cel.StringType}, cel.DynType,
				cel.UnaryBinding(fromEnvBinding))),
	}
}

// toEnvBinding renders a flat object as .env content. An env file is a flat
// name->value map, so a nested object or list is refused by name rather than
// rendered as something that would not parse back.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func toEnvBinding(val ref.Val) ref.Val {
	native, err := val.ConvertToNative(structValueType)
	if err != nil {
		return types.NewErr("toEnv: %v", err)
	}
	sv, ok := native.(*structpb.Value)
	if !ok {
		return types.NewErr("toEnv: unexpected value type %T", native)
	}
	fields, ok := sv.AsInterface().(map[string]any)
	if !ok {
		return types.NewErr("toEnv: value must be an object")
	}
	values, err := envScalars(fields)
	if err != nil {
		return types.NewErr("toEnv: %v", err)
	}
	rendered, err := dotenv.Format(values)
	if err != nil {
		return types.NewErr("toEnv: %v", err)
	}
	return types.String(rendered)
}

// envScalars renders every field as its env value, rejecting the composites an
// env file has no way to express. Keys are visited in sorted order so a value
// with two problems always reports the same one.
func envScalars(fields map[string]any) (map[string]string, error) {
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	values := make(map[string]string, len(fields))
	for _, key := range keys {
		switch v := fields[key].(type) {
		case map[string]any:
			return nil, fmt.Errorf("value of %q must be a scalar, got an object", key)
		case []any:
			return nil, fmt.Errorf("value of %q must be a scalar, got a list", key)
		case nil:
			values[key] = ""
		default:
			values[key] = fmt.Sprint(v)
		}
	}
	return values, nil
}

// fromEnvBinding parses .env content into an object of string values. It goes
// through the shared parser, so an expression agrees with how the runtime reads
// the .env files it loads at startup.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func fromEnvBinding(val ref.Val) ref.Val {
	s, ok := val.Value().(string)
	if !ok {
		return types.NewErr("fromEnv: argument must be a string, got %v", val.Type())
	}
	parsed, err := dotenv.Parse([]byte(s))
	if err != nil {
		return types.NewErr("fromEnv: %v", err)
	}
	out := make(map[string]any, len(parsed))
	for key, value := range parsed {
		out[key] = value
	}
	return types.DefaultTypeAdapter.NativeToValue(out)
}
