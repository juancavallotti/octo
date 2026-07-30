package expr

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/ext"
)

// The pinned version of each cel-go library registered below. Every one of them
// gates its functions on a version number and treats "unset" as "whatever the
// linked cel-go ships", so leaving them unset would let a dependency bump widen —
// or redefine — what an already-deployed expression means. Pinning turns that into
// a deliberate edit here, reviewed alongside the docs and the editor catalogue that
// describe the same vocabulary. These are the current maxima in cel-go v0.28.1;
// libraries that have never been versioned sit at 0.
const (
	optionalTypesVersion        = 2
	stringsVersion              = 5
	listsVersion                = 3
	encodersVersion             = 0
	mathVersion                 = 2
	twoVarComprehensionsVersion = 0
	setsVersion                 = 0
	regexVersion                = 0
)

func registerStandardExtensions() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return standardExtensionOptions() })
}

// standardExtensionOptions returns the cel-go utility libraries every Octo
// expression is compiled with, on top of the CEL standard library. They are
// registered through the same seam as octo's own functions, so a message
// expression, a source payload, and a template's {{ }} span all see the same
// vocabulary.
//
// Deliberately absent: ext.Bindings (cel.bind() buys readability, not capability)
// and ext.Protos/ext.NativeTypes, which describe protobuf and Go types the
// JSON-only message contract never carries.
func standardExtensionOptions() []cel.EnvOption {
	return []cel.EnvOption{
		// regex.extract returns optional<string>, so the optional type has to be
		// declared for the regex library to compile at all.
		cel.OptionalTypes(cel.OptionalTypesVersion(optionalTypesVersion)),
		ext.Strings(ext.StringsVersion(stringsVersion)),
		ext.Lists(ext.ListsVersion(listsVersion)),
		ext.Encoders(ext.EncodersVersion(encodersVersion)),
		ext.Math(ext.MathVersion(mathVersion)),
		ext.TwoVarComprehensions(ext.TwoVarComprehensionsVersion(twoVarComprehensionsVersion)),
		ext.Sets(ext.SetsVersion(setsVersion)),
		ext.Regex(ext.RegexVersion(regexVersion)),
	}
}
