package expr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
)

// ErrUnterminatedTemplate reports a {{ with no matching }} in a template.
var ErrUnterminatedTemplate = errors.New("unterminated {{ in template")

// TemplateVars are the variables a template's {{ }} spans may reference: the union
// of every scope a template can be rendered from, since the same template resource
// may be rendered by a message expression (body, vars, env, …) or by a source
// payload (now, settings). A span naming a variable that is not in the scope it is
// actually rendered from fails at render, not at parse — the parse cannot know
// which scope will reach it.
var TemplateVars = unionVars(MessageVars, SourcePayloadVars)

// unionVars concatenates variable sets, dropping the duplicates the scopes share.
func unionVars(sets ...[]string) []string {
	seen := make(map[string]bool)
	var union []string
	for _, set := range sets {
		for _, name := range set {
			if seen[name] {
				continue
			}
			seen[name] = true
			union = append(union, name)
		}
	}
	return union
}

// segment is one piece of a parsed template: literal text, or a compiled CEL
// expression (from a {{ ... }} span) evaluated per render.
type segment struct {
	literal string
	program *Program // nil for a literal segment
}

// Template is a parsed text template — a sequence of literal and expression
// segments. It is immutable after parsing and safe for concurrent rendering.
type Template struct {
	segments []segment
}

// ParseTemplate splits text into literal and {{ expression }} segments, compiling
// each expression once (with TemplateVars) so a malformed expression fails at
// build time rather than per render.
func ParseTemplate(text string) (*Template, error) {
	var segments []segment
	rest := text
	for {
		open := strings.Index(rest, "{{")
		if open < 0 {
			if rest != "" {
				segments = append(segments, segment{literal: rest})
			}
			return &Template{segments: segments}, nil
		}
		if open > 0 {
			segments = append(segments, segment{literal: rest[:open]})
		}
		after := rest[open+2:]
		closeIdx := strings.Index(after, "}}")
		if closeIdx < 0 {
			return nil, ErrUnterminatedTemplate
		}
		program, err := Compile(strings.TrimSpace(after[:closeIdx]), TemplateVars...)
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment{program: program})
		rest = after[closeIdx+2:]
	}
}

// Render evaluates each expression segment against act and concatenates the
// results with the literal segments.
func (t *Template) Render(act map[string]any) (string, error) {
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

// TemplateRegistry loads and caches parsed templates by id so repeated renders and
// concurrent workers reuse one compiled template. It is safe for concurrent use.
type TemplateRegistry struct {
	loader core.ResourceLoader
	mu     sync.Mutex
	cache  map[string]*Template
}

// NewTemplateRegistry returns a registry backed by loader, falling back to the
// no-op loader (every template missing) when loader is nil.
func NewTemplateRegistry(loader core.ResourceLoader) *TemplateRegistry {
	if loader == nil {
		loader = core.NoopResourceLoader{}
	}
	return &TemplateRegistry{loader: loader, cache: make(map[string]*Template)}
}

// Get returns the parsed template for id, loading and parsing it on first use and
// caching it thereafter. Unlike a missing env resource, a referenced template that
// does not exist is an error.
func (r *TemplateRegistry) Get(ctx context.Context, id string) (*Template, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if tpl, ok := r.cache[id]; ok {
		return tpl, nil
	}
	data, err := r.loader.Load(ctx, core.ResourceKindTemplate, id)
	if err != nil {
		return nil, fmt.Errorf("load template %q: %w", id, err)
	}
	tpl, err := ParseTemplate(string(data))
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", id, err)
	}
	r.cache[id] = tpl
	return tpl, nil
}
