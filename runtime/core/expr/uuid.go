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
// `now`, and that is worth saying out loud rather than discovering: an expression
// containing uuid() evaluates to something different every time, so a replayed
// trace does not reproduce, a `validate` rule written on it cannot be reasoned
// about, and a cache key built from it is a cache that never hits. Reach for it
// where a fresh value is the point, and for nothing else.
//
// In particular it is the wrong tool for naming a conversation. An `ai-agent`'s
// memoryThreadId is evaluated once per run, so a minted thread loads a transcript
// nobody wrote and saves one nobody will read — which is what leaving the field
// out already does, without the writes. See vars.toolScope for the scope a tool
// branch is handed.
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
// The standard library's uuid, which is where this belongs: the version and
// variant nibbles RFC 9562 wants are not worth hand-stamping, and a language
// that ships them makes a dependency for it hard to justify. It replaces
// google/uuid, which this reached for only because the runtime predated go1.27
// and the module already had it in its graph.
//
// NewV4 rather than New: New is documented as "equivalent to NewV4 at this
// time", which is an equivalence that may be withdrawn, and what an expression
// language promises its users is a specific thing rather than a current one.
//
// There is no error to handle. google/uuid returned the entropy source's failure
// and this returned it as a CEL error; the standard library treats a failing
// system CSPRNG as unrecoverable and does not offer the choice, so the binding
// is the one line it looks like.
//
//nolint:ireturn // a CEL function binding returns the ref.Val interface by contract
func uuidBinding(_ ...ref.Val) ref.Val {
	return types.String(uuid.NewV4().String())
}
