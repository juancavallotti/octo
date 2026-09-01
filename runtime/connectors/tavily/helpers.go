// This file holds helpers shared by the tavily blocks: binding a block to its
// tavily connector, the CEL compile plumbing that mirrors the other CEL-driven
// blocks (notion, pinecone, slack), and the payload helpers the four blocks use
// to assemble Tavily's many optional request fields.
package tavily

import (
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// resolveConnector binds a block to its tavily connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("tavily connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not a tavily connector", name)
	}
	return conn, nil
}

// compileOptional compiles a message CEL expression, returning a nil program for
// an empty source so callers can treat "unset" as "skip".
func compileOptional(res core.ResourceLoader, src string) (*expr.Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	return expr.CompileMessage(res, src)
}

// compileRequired compiles a required message CEL expression, erroring with a
// block- and field-labelled message when it is empty or malformed.
func compileRequired(res core.ResourceLoader, block, field, src string) (*expr.Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("%s requires a %q expression", block, field)
	}
	program, err := expr.CompileMessage(res, src)
	if err != nil {
		return nil, fmt.Errorf("%s: compile %s: %w", block, field, err)
	}
	return program, nil
}

// failOnErrorDefault resolves a *bool failOnError setting, defaulting to true
// when unset (a pointer distinguishes an explicit false from absent).
func failOnErrorDefault(v *bool) bool {
	if v != nil {
		return *v
	}
	return true
}

// onCallError centralizes the "a Tavily error aborts unless tolerated" decision:
// it returns the error when failOnError is set, otherwise the message unchanged
// so the flow continues.
func onCallError(msg *types.Message, err error, failOnError bool) (*types.Message, error) {
	if failOnError {
		return nil, err
	}
	return msg, nil
}

// deliver hands a block's result back to the message: into resultVar when the
// block names one, otherwise as the body.
//
// The body is the default on purpose, matching pinecone: a search's results are
// the payload the next block works on, and an HTTP flow that ends there should
// answer with them. Naming a variable is how you say "keep the body I came in
// with", which is the exception, not the rule.
func deliver(msg *types.Message, resultVar string, result any) *types.Message {
	if resultVar != "" {
		msg.Variables.Set(resultVar, result)
		return msg
	}
	msg.SetBody(result)
	return msg
}

// compileList compiles an optional CEL expression that must evaluate to a list
// of strings, used by every domain- and path-filter setting.
func compileList(res core.ResourceLoader, block, field, src string) (*expr.Program, error) {
	program, err := compileOptional(res, src)
	if err != nil {
		return nil, fmt.Errorf("%s: compile %s: %w", block, field, err)
	}
	return program, nil
}

// putList evaluates an optional list expression and, when it yields anything,
// stores it in the payload under key. A nil program or an empty list leaves the
// field out so Tavily applies its own default.
func putList(payload map[string]any, key string, program *expr.Program, activation map[string]any) error {
	if program == nil {
		return nil
	}
	raw, err := program.Eval(activation)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	items, err := toStringSlice(raw)
	if err != nil {
		return fmt.Errorf("%s %w", key, err)
	}
	if len(items) > 0 {
		payload[key] = items
	}
	return nil
}

// putOptional stores value under key when it is non-zero, so an unset setting
// leaves Tavily's own default in force rather than pinning it to Go's zero.
func putOptional[T comparable](payload map[string]any, key string, value T) {
	var zero T
	if value != zero {
		payload[key] = value
	}
}

// toStringSlice converts an evaluated CEL value to []string. A bare string is
// accepted as a one-element list, which is what tavily-extract's urls setting
// needs — Tavily itself takes either.
func toStringSlice(raw any) ([]string, error) {
	if s, ok := raw.(string); ok {
		return []string{s}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("must evaluate to a string or a list of strings, got %T", raw)
	}
	out := make([]string, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("[%d] is not a string (got %T)", i, item)
		}
		out[i] = s
	}
	return out, nil
}
