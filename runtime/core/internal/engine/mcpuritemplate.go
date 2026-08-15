// This file implements RFC 6570 level-1 URI templates, the only kind an MCP
// resource template needs: literal text with {name} placeholders that each stand
// for exactly one value. Level 1 is a deliberate cut — the operators (+ # . / ;
// ? &), explode and prefix modifiers all change what a match means, so supporting
// them would mean deciding a dozen ambiguities nobody has asked for.
package engine

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// uriTemplateVarPattern is what a placeholder name may be. It is deliberately
// narrower than RFC 6570 allows: the value a placeholder takes is reached from
// the rendered template as body.<name>, so a name that is not a CEL identifier
// would only be reachable as body["contact-id"], which is a trap rather than a
// feature.
var uriTemplateVarPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// uriTemplate is a parsed level-1 template, stored pre-split so matching is a
// walk rather than a parse. literals holds exactly one more element than vars,
// and a matching uri is literals[0] + v0 + literals[1] + v1 + … + literals[n].
type uriTemplate struct {
	raw      string
	literals []string
	vars     []string
}

// parseURITemplate splits a template into its literals and placeholder names.
//
// It rejects, rather than accepts-and-hopes, every shape that could not be
// matched unambiguously or whose value could not be read once matched. A
// template is configuration a person writes once and a client relies on
// thereafter, so the moment to complain is when the flow is built.
func parseURITemplate(raw string) (uriTemplate, error) {
	if !strings.Contains(raw, "{") {
		return uriTemplate{}, fmt.Errorf(
			"uriTemplate %q has no {variable}: use uri for a resource that is one fixed document", raw)
	}

	tpl := uriTemplate{raw: raw}
	seen := make(map[string]bool)
	rest := raw
	for {
		open := strings.Index(rest, "{")
		if open < 0 {
			tpl.literals = append(tpl.literals, rest)
			break
		}
		closed := strings.Index(rest[open:], "}")
		if closed < 0 {
			return uriTemplate{}, fmt.Errorf("uriTemplate %q has an unclosed {", raw)
		}
		closed += open

		literal := rest[:open]
		name := rest[open+1 : closed]
		if err := checkURITemplateVar(raw, name, literal, len(tpl.vars), seen); err != nil {
			return uriTemplate{}, err
		}
		seen[name] = true
		tpl.literals = append(tpl.literals, literal)
		tpl.vars = append(tpl.vars, name)
		rest = rest[closed+1:]
	}
	return tpl, nil
}

// checkURITemplateVar validates one placeholder in the position it was found.
// The empty-literal rule is the subtle one: two adjacent placeholders have
// nothing between them to anchor on, so "{a}{b}" could be split anywhere.
func checkURITemplateVar(raw, name, literal string, index int, seen map[string]bool) error {
	switch {
	case strings.Contains(name, "{"):
		return fmt.Errorf("uriTemplate %q has a nested {", raw)
	case name == "":
		return fmt.Errorf("uriTemplate %q has an empty {} placeholder", raw)
	case !uriTemplateVarPattern.MatchString(name):
		return fmt.Errorf(
			"uriTemplate %q variable %q must be a letter or underscore followed by letters, digits or "+
				"underscores: its value is read as body.%s in the resource's template", raw, name, name)
	case seen[name]:
		return fmt.Errorf("uriTemplate %q declares {%s} more than once", raw, name)
	case literal == "" && index > 0:
		return fmt.Errorf(
			"uriTemplate %q has two placeholders with nothing between them: a uri matching it could be "+
				"split more than one way", raw)
	}
	return nil
}

// match reports whether uri is an expansion of this template and, if so, what
// each placeholder took.
//
// A placeholder matches up to the NEXT literal rather than greedily, which is
// what makes "contacts://contact/{id}/notes" take one segment instead of
// swallowing the rest. It never matches the empty string: an empty expansion
// would make "contacts://contact/" a contact, and it is not one. Values are
// percent-decoded, since level-1 expansion encodes everything outside the
// unreserved set.
func (t uriTemplate) match(uri string) (map[string]any, bool) {
	rest, ok := strings.CutPrefix(uri, t.literals[0])
	if !ok {
		return nil, false
	}

	values := make(map[string]any, len(t.vars))
	for i, name := range t.vars {
		next := t.literals[i+1]
		raw, remainder, ok := cutValue(rest, next)
		if !ok {
			return nil, false
		}
		decoded, err := url.PathUnescape(raw)
		if err != nil {
			return nil, false
		}
		values[name] = decoded
		rest = remainder
	}
	// Anything left over is a uri that merely starts like this template.
	if rest != "" {
		return nil, false
	}
	return values, true
}

// cutValue takes the placeholder value that ends at the next literal, returning
// it and what follows. A trailing placeholder (an empty next literal) takes the
// whole remainder.
func cutValue(rest, next string) (value, remainder string, ok bool) {
	if next == "" {
		if rest == "" {
			return "", "", false
		}
		return rest, "", true
	}
	at := strings.Index(rest, next)
	// at == 0 would be an empty value, which is not a match.
	if at <= 0 {
		return "", "", false
	}
	return rest[:at], rest[at+len(next):], true
}
