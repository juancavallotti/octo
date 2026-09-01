// This file holds helpers shared by the parallel blocks: binding a block to its
// parallel connector, and the CEL compile plumbing that mirrors the other
// CEL-driven blocks (notion, tavily, pinecone, slack).
package parallel

import (
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// resolveConnector binds a block to its parallel connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("parallel connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not a parallel connector", name)
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

// orDefault returns value when it is non-empty, otherwise fallback.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// failOnErrorDefault resolves a *bool failOnError setting, defaulting to true
// when unset (a pointer distinguishes an explicit false from absent).
func failOnErrorDefault(v *bool) bool {
	if v != nil {
		return *v
	}
	return true
}

// onCallError centralizes the "a Parallel error aborts unless tolerated"
// decision: it returns the error when failOnError is set, otherwise the message
// unchanged so the flow continues.
func onCallError(msg *types.Message, err error, failOnError bool) (*types.Message, error) {
	if failOnError {
		return nil, err
	}
	return msg, nil
}

// deliver hands a block's result back to the message: into resultVar when the
// block names one, otherwise as the body.
//
// The body is the default on purpose, matching pinecone and tavily: a search's
// results are the payload the next block works on, and an HTTP flow that ends
// there should answer with them. Naming a variable is how you say "keep the body
// I came in with", which is the exception, not the rule.
func deliver(msg *types.Message, resultVar string, result any) *types.Message {
	if resultVar != "" {
		msg.Variables.Set(resultVar, result)
		return msg
	}
	msg.SetBody(result)
	return msg
}

// putOptional stores value under key when it is non-zero, so an unset setting
// leaves Parallel's own default in force rather than pinning it to Go's zero.
func putOptional[T comparable](payload map[string]any, key string, value T) {
	var zero T
	if value != zero {
		payload[key] = value
	}
}

// toStringSlice converts an evaluated CEL value to []string. A bare string is
// accepted as a one-element list, which is what a single search query means.
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
