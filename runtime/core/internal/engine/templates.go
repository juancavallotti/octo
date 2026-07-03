package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
)

// segment is one piece of a parsed template: literal text, or a compiled CEL
// expression (from a {{ ... }} span) evaluated per render.
type segment struct {
	literal string
	program *expr.Program // nil for a literal segment
}

// template is a parsed text template — a sequence of literal and expression
// segments. It is immutable after parsing and safe for concurrent rendering,
// matching the MessageProcessor thread-safety contract.
type template struct {
	segments []segment
}

// errUnterminatedTemplate reports a {{ with no matching }}.
var errUnterminatedTemplate = errors.New("unterminated {{ in template")

// parseTemplate splits text into literal and {{ expression }} segments, compiling
// each expression once (with exprVarNames) so a malformed expression fails at
// build time rather than per message.
func parseTemplate(text string) (*template, error) {
	var segments []segment
	rest := text
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			if rest != "" {
				segments = append(segments, segment{literal: rest})
			}
			return &template{segments: segments}, nil
		}
		if open > 0 {
			segments = append(segments, segment{literal: rest[:open]})
		}
		after := rest[open+2:]
		closeIdx := strings.Index(after, "}}")
		if closeIdx < 0 {
			return nil, errUnterminatedTemplate
		}
		program, err := expr.Compile(strings.TrimSpace(after[:closeIdx]), exprVarNames...)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment{program: program})
		rest = after[closeIdx+2:]
	}
}

// render evaluates each expression segment against act and concatenates the
// results with the literal segments.
func (t *template) render(act map[string]any) (string, error) {
	var b strings.Builder
	for _, seg := range t.segments {
		if seg.program == nil {
			b.WriteString(seg.literal)
			continue
		}
		value, err := seg.program.EvalString(act)
		if err != nil {
			return "", err
		}
		b.WriteString(value)
	}
	return b.String(), nil
}

// templateRegistry loads and caches parsed templates by id so repeated renders and
// concurrent workers reuse one compiled template. It is safe for concurrent use.
type templateRegistry struct {
	loader core.ResourceLoader
	mu     sync.Mutex
	cache  map[string]*template
}

// newTemplateRegistry returns a registry backed by loader, falling back to the
// no-op loader (every template missing) when loader is nil.
func newTemplateRegistry(loader core.ResourceLoader) *templateRegistry {
	if loader == nil {
		loader = core.NoopResourceLoader{}
	}
	return &templateRegistry{loader: loader, cache: make(map[string]*template)}
}

// get returns the parsed template for id, loading and parsing it on first use and
// caching it thereafter. Unlike a missing env resource, a referenced template that
// does not exist is an error.
func (r *templateRegistry) get(ctx context.Context, id string) (*template, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tpl, ok := r.cache[id]; ok {
		return tpl, nil
	}
	data, err := r.loader.Load(ctx, core.ResourceKindTemplate, id)
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", id, err)
	}
	tpl, err := parseTemplate(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", id, err)
	}
	r.cache[id] = tpl
	return tpl, nil
}
