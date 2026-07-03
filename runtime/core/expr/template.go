package expr

import (
	"fmt"
	"reflect"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common"
	"github.com/google/cel-go/common/ast"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// templateFuncName is the CEL function that renders a template resource against
// the current message.
const templateFuncName = "templateResource"

// mapStringAnyType is the conversion target used to read the injected context map
// out of a CEL value.
var mapStringAnyType = reflect.TypeFor[map[string]any]()

// TemplateResolver renders the template resource id against ctx — the variables in
// scope where templateResource(id) was called (body, vars, env, ...). It returns
// the rendered text or an error the CEL evaluation surfaces.
type TemplateResolver func(id string, ctx map[string]any) (string, error)

// templateResourceOptions builds the macro + function that implement the
// templateResource(id) message extension against resolve. Because a CEL function
// binding cannot read the evaluation activation, a global macro rewrites
// templateResource(id) into a two-argument call templateResource(id, {"body":
// body, ...}) that hands the in-scope variables to the binding, which renders the
// template via resolve. vars are the variable names in scope.
func templateResourceOptions(vars []string, resolve TemplateResolver) []cel.EnvOption {
	return []cel.EnvOption{
		cel.Macros(cel.GlobalMacro(templateFuncName, 1, templateMacro(vars))),
		cel.Function(templateFuncName,
			cel.Overload(templateFuncName+"_string_map",
				[]*cel.Type{cel.StringType, cel.MapType(cel.StringType, cel.DynType)},
				cel.StringType,
				cel.BinaryBinding(templateBinding(resolve)))),
	}
}

// templateMacro expands templateResource(id) into templateResource(id, ctx) where
// ctx is a map literal of the in-scope variable names to their values, so the
// function binding receives the activation it cannot otherwise reach.
func templateMacro(vars []string) cel.MacroFactory {
	return func(ef cel.MacroExprFactory, _ ast.Expr, args []ast.Expr) (ast.Expr, *common.Error) {
		entries := make([]ast.EntryExpr, 0, len(vars))
		for _, name := range vars {
			entries = append(entries, ef.NewMapEntry(ef.NewLiteral(types.String(name)), ef.NewIdent(name), false))
		}
		return ef.NewCall(templateFuncName, args[0], ef.NewMap(entries...)), nil
	}
}

// templateBinding renders the template via resolve, converting CEL values to and
// from native Go and surfacing any error as a CEL error.
func templateBinding(resolve TemplateResolver) func(ref.Val, ref.Val) ref.Val {
	return func(idVal, ctxVal ref.Val) ref.Val {
		id, ok := idVal.Value().(string)
		if !ok {
			return types.NewErr("templateResource: id must be a string, got %v", idVal.Type())
		}
		native, err := ctxVal.ConvertToNative(mapStringAnyType)
		if err != nil {
			return types.NewErr("templateResource: context: %v", err)
		}
		// cel-go converts nested maps to map[any]any; normalize to JSON-native
		// map[string]any so the rendered template sees the same shapes as the rest
		// of the runtime (scalars and time.Time are left untouched).
		ctx, _ := normalizeNative(native).(map[string]any)
		out, err := resolve(id, ctx)
		if err != nil {
			return types.NewErr("%v", err)
		}
		return types.String(out)
	}
}

// normalizeNative rewrites cel-go's native conversion output into JSON-native
// shapes: map[any]any (how cel renders nested maps) becomes map[string]any, and
// nested slices/maps are converted recursively. Scalars and other types (e.g.
// time.Time for now) pass through unchanged.
func normalizeNative(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			out[k] = normalizeNative(e)
		}
		return out
	case map[any]any:
		out := make(map[string]any, len(val))
		for k, e := range val {
			out[fmt.Sprint(k)] = normalizeNative(e)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, e := range val {
			out[i] = normalizeNative(e)
		}
		return out
	default:
		return v
	}
}
