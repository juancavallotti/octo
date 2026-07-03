package expr

import (
	"context"

	"github.com/google/cel-go/cel"

	"github.com/juancavallotti/octo/core"
)

// MessageVars is the canonical set of variables every message expression may
// reference: the decoded body, the message variables, the two identifiers, the
// resolved environment, and the evaluation time. Blocks and connectors compile
// message expressions through CompileMessage rather than naming these themselves,
// so the set stays defined in exactly one place.
var MessageVars = []string{"body", "vars", "eventID", "correlationID", "env", "now"}

// MessageContext carries the per-compile inputs a message extension may need.
type MessageContext struct {
	// Resources loads resources (e.g. templates) a function needs. It is never nil
	// inside an extension: CompileMessage substitutes the no-op loader when unset.
	Resources core.ResourceLoader
}

// MessageExtension contributes CEL environment options (custom functions, macros)
// to every message expression, given the compile context. Registering one exposes
// a new capability to every message expression at once — no call-site changes.
type MessageExtension func(MessageContext) []cel.EnvOption

// messageExtensions holds the registered extensions, applied in order by
// CompileMessage. It is populated only from package init, so it is effectively
// immutable after startup and needs no locking.
var messageExtensions []MessageExtension

// RegisterMessageExtension adds ext to the extensions every message expression is
// compiled with. Call it from an init function; this is the single place a new CEL
// capability is wired in for the whole runtime.
func RegisterMessageExtension(ext MessageExtension) {
	messageExtensions = append(messageExtensions, ext)
}

// CompileMessage compiles a message expression: it declares the standard message
// variables (MessageVars) and applies every registered extension bound to res.
// Every block and connector that evaluates CEL against a message compiles through
// this, so a capability added via RegisterMessageExtension becomes available
// everywhere without editing call sites. res may be nil (a no-op loader is used).
func CompileMessage(res core.ResourceLoader, expression string) (*Program, error) {
	if res == nil {
		res = core.NoopResourceLoader{}
	}
	mc := MessageContext{Resources: res}
	var opts []cel.EnvOption
	for _, ext := range messageExtensions {
		opts = append(opts, ext(mc)...)
	}
	return CompileWithOptions(expression, MessageVars, opts...)
}

func init() {
	// templateResource(id) renders a template resource against the current message.
	// A fresh registry per compiled expression caches parsed templates for that
	// expression; the loader itself is shared across the generation.
	RegisterMessageExtension(func(mc MessageContext) []cel.EnvOption {
		reg := NewTemplateRegistry(mc.Resources)
		resolve := func(id string, ctx map[string]any) (string, error) {
			tpl, err := reg.Get(context.Background(), id)
			if err != nil {
				return "", err
			}
			return tpl.Render(ctx)
		}
		return templateResourceOptions(MessageVars, resolve)
	})
}
