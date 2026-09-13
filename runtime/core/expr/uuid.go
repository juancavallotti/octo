package expr

import (
	"uuid"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// A random identifier, for the places a message needs one and nothing upstream
// supplied it: a correlation id on an outbound request, an idempotency key, a
// synthetic id for a record that arrived without one, a scratch name.
//
// It is the second non-deterministic thing in the expression language, after
// `now`: an expression containing uuid() evaluates to something different every
// time, so a replayed trace does not reproduce, a validation rule written on it
// cannot be reasoned about, and a cache key built from it is a cache that never
// hits. Reach for it where a fresh value is the point, and for nothing else — in
// particular, never to name something that must be found again.
const uuidFuncName = "uuid"

func registerUUIDExtension() {
	RegisterMessageExtension(func(MessageContext) []cel.EnvOption { return uuidOptions() })
}

// uuidOptions declares the zero-argument uuid() function. It reads nothing from
// the activation — the whole value is the entropy — so a plain function binding
// with no arguments suffices.
func uuidOptions() []cel.EnvOption {
	return []cel.EnvOption{
		cel.Function(uuidFuncName,
			cel.Overload(uuidFuncName+"_string", nil, cel.StringType,
				cel.FunctionBinding(uuidBinding))),
	}
}

// uuidBinding renders a version 4 UUID.
//
// NewV4 rather than New: New is documented as "equivalent to NewV4 at this time",
// an equivalence that may be withdrawn, and what an expression language promises
// its users is a specific thing rather than a current one.
//
// There is no error to handle: a failing system CSPRNG is unrecoverable, and the
// standard library does not offer the choice.
//
//nolint:ireturn // a CEL function binding returns the ref.Val interface by contract
func uuidBinding(_ ...ref.Val) ref.Val {
	return types.String(uuid.NewV4().String())
}
