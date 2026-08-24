package expr

import (
	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"github.com/google/uuid"
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
// google/uuid rather than sixteen bytes and a format string: it is already in this
// module's graph (the pinecone connector's client depends on it), so this promotes
// an indirect dependency rather than adding one — and the version and variant
// nibbles RFC 9562 wants are exactly the detail not worth hand-stamping.
//
// NewRandom's error is the entropy source failing, and it is returned as a CEL
// error rather than swallowed into a zero value: an id that is silently not unique
// is worse than an expression that fails, because the failure is visible and the
// collision is not.
//
//nolint:ireturn // a CEL function binding returns the ref.Val interface by contract
func uuidBinding(_ ...ref.Val) ref.Val {
	id, err := uuid.NewRandom()
	if err != nil {
		return types.NewErr("uuid: %v", err)
	}
	return types.String(id.String())
}
