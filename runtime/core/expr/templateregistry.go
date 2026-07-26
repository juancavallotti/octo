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
// payload (now, settings). The parse cannot know which scope will reach it, so
// Render binds the names the actual scope does not supply to null rather than
// leaving them unbound — see Render for why that has to be so.
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
// build time rather than per render. res backs the resource-loading functions a
// span may call — templateResource(id) among them — and is captured at compile
// time, so it must be the loader the template will render against.
func ParseTemplate(text string, res core.ResourceLoader) (*Template, error) {
	return parseTemplate(text, res, 0)
}

// parseTemplate is ParseTemplate at a known nesting depth: depth is how many
// templates deep the text already sits, which the spans carry into their compile
// context so templateResource can bound recursion.
func parseTemplate(text string, res core.ResourceLoader, depth int) (*Template, error) {
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
		// Through the same seam as message expressions and source payloads, so a
		// span reaches every registered capability — toJson, templateResource and
		// whatever is added next — rather than plain CEL plus the variables.
		program, err := compileWithExtensions(MessageContext{
			Resources:     res,
			Vars:          TemplateVars,
			TemplateDepth: depth,
		}, strings.TrimSpace(after[:closeIdx]))
		if err != nil {
			return nil, err
		}
		segments = append(segments, segment{program: program})
		rest = after[closeIdx+2:]
	}
}

// Render evaluates each expression segment against act and concatenates the
// results with the literal segments. act is padded to the full TemplateVars set
// first: a span calling templateResource expands into a map literal naming every
// variable in scope, and a template compiled for the union of scopes therefore
// names "settings" even when a message renders it. Leaving those unbound would
// fail the map literal — and so the call — for a template that never mentions
// them. The cost is that a span reading a variable its scope does not supply now
// sees null instead of failing outright.
func (t *Template) Render(act map[string]any) (string, error) {
	act = withTemplateVars(act)
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

// withTemplateVars returns act with every TemplateVars name bound, adding the
// ones the caller's scope does not supply as null. act is copied rather than
// filled in place: the caller's activation is theirs, and MessageActivation's
// result is shared across the segments of one render.
func withTemplateVars(act map[string]any) map[string]any {
	missing := 0
	for _, name := range TemplateVars {
		if _, ok := act[name]; !ok {
			missing++
		}
	}
	if missing == 0 {
		return act
	}
	full := make(map[string]any, len(act)+missing)
	for k, v := range act {
		full[k] = v
	}
	for _, name := range TemplateVars {
		if _, ok := full[name]; !ok {
			full[name] = nil
		}
	}
	return full
}

// TemplateRegistry loads and caches parsed templates by id so repeated renders and
// concurrent workers reuse one compiled template. It is safe for concurrent use.
type TemplateRegistry struct {
	loader core.ResourceLoader
	// depth is how many templates deep the templates this registry parses sit, so
	// a template that renders another counts towards maxTemplateDepth.
	depth int
	mu    sync.Mutex
	cache map[string]*Template
}

// NewTemplateRegistry returns a registry backed by loader, falling back to the
// no-op loader (every template missing) when loader is nil. Its templates are
// top-level: they are rendered by a message expression or a source payload, not
// by another template.
func NewTemplateRegistry(loader core.ResourceLoader) *TemplateRegistry {
	return newTemplateRegistry(loader, 0)
}

// newTemplateRegistry returns a registry whose templates sit depth templates deep.
func newTemplateRegistry(loader core.ResourceLoader, depth int) *TemplateRegistry {
	if loader == nil {
		loader = core.NoopResourceLoader{}
	}
	return &TemplateRegistry{loader: loader, depth: depth, cache: make(map[string]*Template)}
}

// Get returns the parsed template for id, loading and parsing it on first use and
// caching it thereafter. Unlike a missing env resource, a referenced template that
// does not exist is an error.
//
// Parsing under r.mu is safe: it only compiles, and compiling never calls Get. A
// nested template is fetched at render, from the deeper registry its parent's
// spans were compiled with.
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
	tpl, err := parseTemplate(string(data), r.loader, r.depth)
	if err != nil {
		return nil, fmt.Errorf("parse template %q: %w", id, err)
	}
	r.cache[id] = tpl
	return tpl, nil
}
