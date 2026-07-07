// This file holds helpers shared by the notion blocks: binding a block to its
// notion connector, and the CEL activation plumbing that mirrors the other
// CEL-driven blocks (slack, rest, queue-dispatch).
package notion

import (
	"fmt"
	"strings"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
)

// resolveConnector binds a block to its notion connector by name.
func resolveConnector(name string, deps core.BlockDeps) (*Connector, error) {
	if name == "" {
		return nil, fmt.Errorf("connector is required")
	}
	if deps.Connector == nil {
		return nil, fmt.Errorf("connector %q requested but no connectors are available", name)
	}
	connector, ok := deps.Connector(name)
	if !ok {
		return nil, fmt.Errorf("notion connector %q is not configured", name)
	}
	conn, ok := connector.(*Connector)
	if !ok {
		return nil, fmt.Errorf("connector %q is not a notion connector", name)
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
