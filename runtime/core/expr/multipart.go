package expr

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/cel-go/common/types/traits"

	octotypes "github.com/juancavallotti/octo/runtime/types"
)

// The multipart functions. fromMultipart parses a raw multipart payload into the
// parts map; multipart/addPart build one up; toMultipart renders one back to a
// string. Every one of them speaks the same parts map the http source puts on
// body.parts, so a decoded upload can be forwarded, augmented, or rebuilt without
// converting between representations.
//
// addPart is Octo's first member-style function — the others are all global. The
// builder chains, and a global form would nest inside out
// (addPart(addPart(multipart(), ...), ...)) exactly where readability matters most.
const (
	multipartFuncName     = "multipart"
	addPartFuncName       = "addPart"
	fromMultipartFuncName = "fromMultipart"
	toMultipartFuncName   = "toMultipart"
)

func registerMultipartExtension() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return multipartOptions() })
}

// multipartOptions declares the four multipart functions. Like the json and
// formdata functions they act purely on their arguments, so no activation access
// is needed and plain bindings suffice.
func multipartOptions() []cel.EnvOption {
	partsType := cel.MapType(cel.StringType, cel.DynType)
	return []cel.EnvOption{
		cel.Function(multipartFuncName,
			cel.Overload(multipartFuncName+"_map",
				[]*cel.Type{}, partsType,
				cel.FunctionBinding(multipartBinding))),
		cel.Function(addPartFuncName,
			cel.MemberOverload(addPartFuncName+"_map_string_dyn_map",
				[]*cel.Type{cel.DynType, cel.StringType, cel.DynType}, partsType,
				cel.FunctionBinding(addPartBinding))),
		cel.Function(fromMultipartFuncName,
			cel.Overload(fromMultipartFuncName+"_dyn_map",
				[]*cel.Type{cel.DynType}, partsType,
				cel.UnaryBinding(fromMultipartBodyBinding)),
			cel.Overload(fromMultipartFuncName+"_string_string_map",
				[]*cel.Type{cel.StringType, cel.StringType}, partsType,
				cel.BinaryBinding(fromMultipartBinding))),
		cel.Function(toMultipartFuncName,
			cel.Overload(toMultipartFuncName+"_dyn_string",
				[]*cel.Type{cel.DynType}, cel.StringType,
				cel.UnaryBinding(toMultipartDefaultBinding)),
			cel.Overload(toMultipartFuncName+"_dyn_string_string",
				[]*cel.Type{cel.DynType, cel.StringType}, cel.StringType,
				cel.BinaryBinding(toMultipartBinding))),
	}
}

// multipartBinding returns an empty parts map to build on.
//
//nolint:ireturn // a CEL FunctionBinding returns the ref.Val interface
func multipartBinding(_ ...ref.Val) ref.Val {
	return types.DefaultTypeAdapter.NativeToValue(map[string]any{})
}

// addPartBinding returns a *new* parts map with one part added, normalized to the
// full decoded shape so a part built here and a part read off the wire are
// indistinguishable downstream.
//
// It copies rather than inserting in place: CEL values are immutable and one
// expression may reference the same intermediate twice, so mutating the receiver
// would make x.addPart(...) visibly change x. The copy is a map of references.
//
//nolint:ireturn // a CEL FunctionBinding returns the ref.Val interface
func addPartBinding(args ...ref.Val) ref.Val {
	const wantArgs = 3
	if len(args) != wantArgs {
		return types.NewErr("%s: expected a name and a value", addPartFuncName)
	}
	parts, err := nativeParts(args[0], addPartFuncName)
	if err != nil {
		return types.NewErr("%v", err)
	}
	name, ok := args[1].Value().(string)
	if !ok {
		return types.NewErr("%s: name must be a string, got %v", addPartFuncName, args[1].Type())
	}

	part, err := NormalizePart(name, nativeValue(args[2]))
	if err != nil {
		return types.NewErr("%s: %v", addPartFuncName, err)
	}

	updated := make(map[string]any, len(parts)+1)
	for key, value := range parts {
		updated[key] = value
	}
	addNamed(updated, name, part)
	return types.DefaultTypeAdapter.NativeToValue(updated)
}

// fromMultipartBodyBinding decodes a raw-content body, reading the boundary out of
// the contentType it carries. This is the common call — fromMultipart(body).
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func fromMultipartBodyBinding(val ref.Val) ref.Val {
	body, ok := nativeValue(val).(map[string]any)
	if !ok {
		return types.NewErr("%s: argument must be a raw-content body or a (data, contentType) pair",
			fromMultipartFuncName)
	}
	rawData, ok := body[octotypes.RawDataKey].(string)
	if !ok {
		return types.NewErr("%s: body carries no %s string", fromMultipartFuncName, octotypes.RawDataKey)
	}
	contentType, ok := body[octotypes.RawContentTypeKey].(string)
	if !ok {
		return types.NewErr("%s: body carries no %s string", fromMultipartFuncName, octotypes.RawContentTypeKey)
	}
	return decodeToValue(rawData, contentType)
}

// fromMultipartBinding decodes an explicit (rawData, contentType) pair, for a
// payload whose content type is not on the body — a queue message that carried it
// in a variable, say.
//
//nolint:ireturn // a CEL BinaryBinding returns the ref.Val interface
func fromMultipartBinding(data, contentType ref.Val) ref.Val {
	raw, ok := data.Value().(string)
	if !ok {
		return types.NewErr("%s: data must be a string, got %v", fromMultipartFuncName, data.Type())
	}
	mediaType, ok := contentType.Value().(string)
	if !ok {
		return types.NewErr("%s: contentType must be a string, got %v",
			fromMultipartFuncName, contentType.Type())
	}
	return decodeToValue(raw, mediaType)
}

// decodeToValue is the shared tail of both fromMultipart overloads.
//
//nolint:ireturn // a CEL binding returns the ref.Val interface
func decodeToValue(rawData, contentType string) ref.Val {
	parts, err := DecodeMultipart(rawData, contentType)
	if err != nil {
		return types.NewErr("%s: %v", fromMultipartFuncName, err)
	}
	return types.DefaultTypeAdapter.NativeToValue(parts)
}

// toMultipartDefaultBinding renders with the fixed boundary, which a flow serving
// the result names in set-payload's static contentType field.
//
//nolint:ireturn // a CEL UnaryBinding returns the ref.Val interface
func toMultipartDefaultBinding(val ref.Val) ref.Val {
	return encodeToValue(val, defaultMultipartBoundary)
}

// toMultipartBinding renders with a caller-supplied boundary.
//
//nolint:ireturn // a CEL BinaryBinding returns the ref.Val interface
func toMultipartBinding(val, boundary ref.Val) ref.Val {
	name, ok := boundary.Value().(string)
	if !ok {
		return types.NewErr("%s: boundary must be a string, got %v",
			toMultipartFuncName, boundary.Type())
	}
	return encodeToValue(val, name)
}

// encodeToValue is the shared tail of both toMultipart overloads.
//
//nolint:ireturn // a CEL binding returns the ref.Val interface
func encodeToValue(val ref.Val, boundary string) ref.Val {
	parts, err := nativeParts(val, toMultipartFuncName)
	if err != nil {
		return types.NewErr("%v", err)
	}
	encoded, err := EncodeMultipart(parts, boundary)
	if err != nil {
		return types.NewErr("%s: %v", toMultipartFuncName, err)
	}
	return types.String(encoded)
}

// nativeParts unwraps a CEL value that should be a parts map, naming the calling
// function in the error so a mistyped argument says which call went wrong.
func nativeParts(val ref.Val, function string) (map[string]any, error) {
	parts, ok := nativeValue(val).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected a parts map, got %v", function, val.Type())
	}
	return parts, nil
}

// nativeValue unwraps a CEL value into JSON-native Go kinds.
//
// The other codecs in this package bridge through structpb (see toJSONBinding),
// which cannot be used here: structpb strings must be valid UTF-8, and a raw
// multipart payload routinely is not. Bridging a body through it would corrupt
// exactly the uploads these functions exist to read, so the traversal is done
// directly instead.
func nativeValue(val ref.Val) any {
	switch typed := val.(type) {
	case traits.Mapper:
		return nativeMap(typed)
	case traits.Lister:
		return nativeList(typed)
	default:
		return val.Value()
	}
}

// nativeMap converts a CEL map, keying by the string form of each key since the
// shapes this package handles are all string-keyed.
func nativeMap(mapper traits.Mapper) map[string]any {
	out := make(map[string]any)
	for it := mapper.Iterator(); it.HasNext() == types.True; {
		key := it.Next()
		out[fmt.Sprint(key.Value())] = nativeValue(mapper.Get(key))
	}
	return out
}

// nativeList converts a CEL list, recursing so a repeated part name arrives as a
// list of decoded parts rather than a list of ref.Val.
func nativeList(lister traits.Lister) []any {
	size, ok := lister.Size().Value().(int64)
	if !ok {
		return nil
	}
	out := make([]any, 0, size)
	for i := int64(0); i < size; i++ {
		out = append(out, nativeValue(lister.Get(types.Int(i))))
	}
	return out
}
