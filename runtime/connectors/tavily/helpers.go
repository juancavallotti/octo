// This file holds helpers shared by the tavily blocks: binding a block to its
// tavily connector, the CEL compile plumbing that mirrors the other CEL-driven
// blocks (notion, pinecone, slack), and the payload helpers the four blocks use
// to assemble Tavily's many optional request fields.
package tavily

import (
	"fmt"
	"maps"
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

// traversalConfig is the crawl/map traversal surface, lifted out of the two
// blocks' settings structs. The settings themselves stay per-block — octo tags
// cannot be shared through an embedded struct — but everything downstream of
// them can be, and is.
type traversalConfig struct {
	URL            string
	Instructions   string
	SelectPaths    string
	ExcludePaths   string
	SelectDomains  string
	ExcludeDomains string
	MaxDepth       int
	MaxBreadth     int
	Limit          int
	AllowExternal  *bool
}

// traversal is a compiled traversalConfig: the CEL programs, plus the
// message-independent request fields folded in once at build time.
type traversal struct {
	url            *expr.Program
	instructions   *expr.Program
	selectPaths    *expr.Program
	excludePaths   *expr.Program
	selectDomains  *expr.Program
	excludeDomains *expr.Program
	fixed          map[string]any
}

// compileTraversal compiles the traversal expressions once, so a malformed one
// fails at startup rather than on the first message.
func compileTraversal(res core.ResourceLoader, block string, cfg traversalConfig) (*traversal, error) {
	url, err := compileRequired(res, block, "url", cfg.URL)
	if err != nil {
		return nil, err
	}
	instructions, err := compileOptional(res, cfg.Instructions)
	if err != nil {
		return nil, fmt.Errorf("%s: compile instructions: %w", block, err)
	}
	lists := map[string]*expr.Program{}
	for field, src := range map[string]string{
		"selectPaths":    cfg.SelectPaths,
		"excludePaths":   cfg.ExcludePaths,
		"selectDomains":  cfg.SelectDomains,
		"excludeDomains": cfg.ExcludeDomains,
	} {
		if lists[field], err = compileList(res, block, field, src); err != nil {
			return nil, err
		}
	}

	fixed := map[string]any{}
	putOptional(fixed, "max_depth", cfg.MaxDepth)
	putOptional(fixed, "max_breadth", cfg.MaxBreadth)
	putOptional(fixed, "limit", cfg.Limit)
	if cfg.AllowExternal != nil {
		fixed["allow_external"] = *cfg.AllowExternal
	}

	return &traversal{
		url:            url,
		instructions:   instructions,
		selectPaths:    lists["selectPaths"],
		excludePaths:   lists["excludePaths"],
		selectDomains:  lists["selectDomains"],
		excludeDomains: lists["excludeDomains"],
		fixed:          fixed,
	}, nil
}

// payload evaluates the per-message traversal expressions onto a copy of the
// fixed fields, yielding the request body crawl and map share.
func (t *traversal) payload(activation map[string]any) (map[string]any, error) {
	url, err := t.url.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("url: %w", err)
	}
	payload := maps.Clone(t.fixed)
	payload["url"] = url
	if err := putString(payload, "instructions", t.instructions, activation); err != nil {
		return nil, err
	}
	for key, program := range map[string]*expr.Program{
		"select_paths":    t.selectPaths,
		"exclude_paths":   t.excludePaths,
		"select_domains":  t.selectDomains,
		"exclude_domains": t.excludeDomains,
	} {
		if err := putList(payload, key, program, activation); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

// putString evaluates an optional string expression into the payload, leaving
// the field out when the program is nil or the result is empty.
func putString(payload map[string]any, key string, program *expr.Program, activation map[string]any) error {
	if program == nil {
		return nil
	}
	value, err := program.EvalString(activation)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	if value != "" {
		payload[key] = value
	}
	return nil
}
