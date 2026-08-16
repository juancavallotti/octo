// This file holds helpers shared by the pinecone blocks: binding a block to its
// pinecone connector, and the CEL compile plumbing that mirrors the other
// CEL-driven blocks (notion, slack, rest).
package pinecone

import (
	"fmt"
	"strings"

	sdk "github.com/pinecone-io/go-pinecone/v5/pinecone"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// resolveConnector binds a block to its pinecone connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("pinecone connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not a pinecone connector", name)
	}
	return conn, nil
}

// compileOptional compiles a message CEL expression, returning a nil program
// for an empty source so callers can treat "unset" as "skip".
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

// deliver hands a block's result back to the message: into resultVar when the
// block names one, otherwise as the body.
//
// The body is the default on purpose. A query's matches, a fetch's vectors —
// these are the payload the next block works on, and an HTTP flow that ends
// there should answer with them. Naming a variable is how you say "keep the
// body I came in with", which is the exception, not the rule.
func deliver(msg *types.Message, resultVar string, result any) *types.Message {
	if resultVar != "" {
		msg.Variables.Set(resultVar, result)
		return msg
	}
	msg.SetBody(result)
	return msg
}

// evalNamespace evaluates an optional namespace expression, returning "" (the
// connector's default namespace) when the program is nil.
func evalNamespace(program *expr.Program, activation map[string]any) (string, error) {
	if program == nil {
		return "", nil
	}
	return program.EvalString(activation)
}

// toFloat32Slice converts an evaluated CEL list of numbers to []float32.
func toFloat32Slice(raw any) ([]float32, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("must evaluate to a list of numbers, got %T", raw)
	}
	out := make([]float32, len(items))
	for i, item := range items {
		f, ok := item.(float64)
		if !ok {
			return nil, fmt.Errorf("[%d] is not a number (got %T)", i, item)
		}
		out[i] = float32(f)
	}
	return out, nil
}

// toStringSlice converts an evaluated CEL list of strings to []string.
func toStringSlice(raw any) ([]string, error) {
	items, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("must evaluate to a list of strings, got %T", raw)
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

// evalMetadataFilter evaluates a compiled filter expression to a Pinecone
// metadata filter, shared by pinecone-query and pinecone-delete.
func evalMetadataFilter(program *expr.Program, activation map[string]any) (*sdk.MetadataFilter, error) {
	raw, err := program.Eval(activation)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("filter must evaluate to an object, got %T", raw)
	}
	filter, err := sdk.NewMetadataFilter(m)
	if err != nil {
		return nil, fmt.Errorf("filter: %w", err)
	}
	return filter, nil
}

// vectorFields folds a vector's values/metadata into a plain map, omitting
// whichever the query/fetch settings didn't request (the SDK leaves them nil
// rather than empty). id is not included: the caller already knows it, either
// as the map key (fetch) or a sibling field (query's toMatch).
func vectorFields(v *sdk.Vector) map[string]any {
	fields := map[string]any{}
	if v.Values != nil {
		fields["values"] = *v.Values
	}
	if v.Metadata != nil {
		fields["metadata"] = v.Metadata.AsMap()
	}
	return fields
}
