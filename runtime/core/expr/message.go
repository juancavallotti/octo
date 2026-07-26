package expr

import (
	"context"
	"fmt"

	"github.com/google/cel-go/cel"

	"github.com/juancavallotti/octo/runtime/core"
)

// nowVar is the CEL variable holding the evaluation (or trigger) time. Named once
// so the message and source-payload variable lists and their activations reference
// a single literal rather than repeating it.
const nowVar = "now"

// MessageVars is the canonical set of variables every message expression may
// reference: the decoded body, the message variables, the two identifiers, the
// resolved environment, and the evaluation time. Blocks and connectors compile
// message expressions through CompileMessage rather than naming these themselves,
// so the set stays defined in exactly one place.
var MessageVars = []string{"body", "vars", "eventID", "correlationID", "env", nowVar}

// MessageContext carries the per-compile inputs a message extension may need.
type MessageContext struct {
	// Resources loads resources (e.g. templates) a function needs. It is never nil
	// inside an extension: the compile substitutes the no-op loader when unset.
	Resources core.ResourceLoader
	// Vars are the variable names in scope for the expression being compiled —
	// MessageVars for a message expression, SourcePayloadVars for a source payload,
	// TemplateVars for a template span. An extension that hands the activation to a
	// function (templateResource does, via a macro) must build it from these rather
	// than assuming the message set.
	Vars []string
	// TemplateDepth is how many templates deep this expression is: 0 for a message
	// expression or a source payload, 1 for a span inside a template one of those
	// rendered, and so on. Templates may call templateResource themselves, so the
	// only thing standing between a self-referencing template and an unbounded
	// render is this counter — see maxTemplateDepth.
	TemplateDepth int
}

// MessageExtension contributes CEL environment options (custom functions, macros)
// to every message expression, given the compile context. Registering one exposes
// a new capability to every message expression at once — no call-site changes.
type MessageExtension func(MessageContext) []cel.EnvOption

// messageExtensions holds the registered extensions, applied in order by
// compileWithExtensions. It is populated only from package init, so it is
// effectively immutable after startup and needs no locking.
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
	return compileWithExtensions(MessageContext{Resources: res, Vars: MessageVars}, expression)
}

// compileWithExtensions declares mc.Vars and applies every registered extension,
// bound to mc. It is the single seam through which message expressions, source
// payloads, and template spans reach the registered capabilities, so an extension
// is wired in once and works in every scope. mc.Resources may be nil (a no-op
// loader is used).
func compileWithExtensions(mc MessageContext, expression string) (*Program, error) {
	if mc.Resources == nil {
		mc.Resources = core.NoopResourceLoader{}
	}
	opts := make([]cel.EnvOption, 0, len(messageExtensions))
	for _, ext := range messageExtensions {
		opts = append(opts, ext(mc)...)
	}
	return CompileWithOptions(expression, mc.Vars, opts...)
}

// maxTemplateDepth caps how many templates may nest through templateResource.
// Templates reach the function through the same seam as everything else, so a
// template that renders itself — directly or through a cycle — would otherwise
// recurse until the stack gave out. The id is an expression argument, so the
// cycle cannot be seen at parse time; the depth is carried through the compile
// context and checked at render.
const maxTemplateDepth = 8

func init() {
	// templateResource(id) renders a template resource against the current message.
	// A fresh registry per compiled expression caches parsed templates for that
	// expression; the loader itself is shared across the generation. Templates the
	// registry parses compile one level deeper, so nesting is counted without any
	// shared state — two concurrent renders never see each other's depth.
	RegisterMessageExtension(func(mc MessageContext) []cel.EnvOption {
		depth := mc.TemplateDepth
		reg := newTemplateRegistry(mc.Resources, depth+1)
		resolve := func(id string, ctx map[string]any) (string, error) {
			if depth >= maxTemplateDepth {
				return "", fmt.Errorf("templateResource %q: nested more than %d templates deep (recursive template?)",
					id, maxTemplateDepth)
			}
			tpl, err := reg.Get(context.Background(), id)
			if err != nil {
				return "", err
			}
			return tpl.Render(ctx)
		}
		return templateResourceOptions(mc.Vars, resolve)
	})
}
