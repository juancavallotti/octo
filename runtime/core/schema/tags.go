package schema

import (
	"fmt"
	"strings"
)

// fieldTag is the parsed form of an `octo` struct tag. It carries the
// presentation metadata for a single field; the field's serialized name always
// comes from the sibling `json` tag, not from here.
type fieldTag struct {
	label      string
	typ        string // explicit FieldType override; empty means infer
	required   bool
	hasDefault bool
	defaultRaw string
	enum       []string
	desc       string // escape hatch; normally the description comes from doc comments
	ref        *Reference
	showIf     *ShowIf
}

// parseOctoTag parses an `octo` tag body into a fieldTag. Clauses are
// comma-separated; a clause is either a bare flag ("required") or "key=value".
// Values never contain commas — enum uses "|" as its own separator — so a plain
// comma split is unambiguous. An unknown clause is an error so typos fail loudly
// at generation rather than silently dropping metadata.
func parseOctoTag(tag string) (fieldTag, error) {
	var ft fieldTag
	if strings.TrimSpace(tag) == "" {
		return ft, nil
	}
	for _, clause := range strings.Split(tag, ",") {
		clause = strings.TrimSpace(clause)
		if clause == "" {
			continue
		}
		key, val, hasVal := strings.Cut(clause, "=")
		key = strings.TrimSpace(key)
		switch key {
		case "label":
			ft.label = val
		case "type":
			ft.typ = val
		case "required":
			ft.required = true
		case "default":
			ft.hasDefault = true
			ft.defaultRaw = val
		case "enum":
			ft.enum = strings.Split(val, "|")
		case "desc":
			ft.desc = val
		case "ref":
			ref, err := parseRef(val)
			if err != nil {
				return ft, err
			}
			ft.ref = ref
		case "showIf":
			field, equals, ok := strings.Cut(val, "=")
			if !ok {
				return ft, fmt.Errorf("showIf must be field=value, got %q", val)
			}
			ft.showIf = &ShowIf{Field: field, Equals: equals}
		default:
			if hasVal {
				return ft, fmt.Errorf("unknown octo clause %q", key)
			}
			return ft, fmt.Errorf("unknown octo flag %q", key)
		}
	}
	return ft, nil
}

// parseRef parses a ref= clause value into a Reference. Accepted forms:
// "flow", "template", "connector:<type>", "connector-category:<category>".
func parseRef(val string) (*Reference, error) {
	switch {
	case val == "flow":
		return &Reference{Kind: "flow"}, nil
	case val == "template":
		return &Reference{Kind: "template"}, nil
	case strings.HasPrefix(val, "connector-category:"):
		return &Reference{Kind: "connector", ConnectorCategory: strings.TrimPrefix(val, "connector-category:")}, nil
	case strings.HasPrefix(val, "connector:"):
		return &Reference{Kind: "connector", ConnectorType: strings.TrimPrefix(val, "connector:")}, nil
	default:
		return nil, fmt.Errorf("unknown ref %q (want flow, template, connector:<type>, or connector-category:<cat>)", val)
	}
}
