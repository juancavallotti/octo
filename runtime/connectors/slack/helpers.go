// This file holds helpers shared by the slack blocks: binding a block to its
// slack connector, and the CEL activation plumbing that mirrors the other
// CEL-driven blocks (rest, queue-dispatch).
package slack

import (
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

// fieldChannel is the Slack Web API request field naming a channel or user ID,
// shared by the blocks that post to or act on a conversation.
const fieldChannel = "channel"

// resolveConnector binds a block to its slack connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("slack connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not a slack connector", name)
	}
	return conn, nil
}

// compileOptional compiles a message CEL expression, returning a nil program for
// an empty source so callers can treat "unset" as "skip". res may be nil (e.g. a
// source filter with no resource loader).
func compileOptional(res core.ResourceLoader, src string) (*expr.Program, error) {
	if strings.TrimSpace(src) == "" {
		return nil, nil
	}
	return expr.CompileMessage(res, src)
}

// orDefault returns value when it is non-empty, otherwise fallback.
func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
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

// onCallError centralizes the "a Slack error aborts unless tolerated" decision:
// it returns the error when failOnError is set, otherwise the message unchanged
// so the flow continues.
func onCallError(msg *types.Message, err error, failOnError bool) (*types.Message, error) {
	if failOnError {
		return nil, err
	}
	return msg, nil
}
