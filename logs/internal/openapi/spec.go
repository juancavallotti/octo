package openapi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// spec is the generated description, embedded at build time. Regenerate it with
// `task logs:openapi`; CI fails on a diff.
//
//go:embed swagger.json
var spec []byte

// Spec returns the full API description as OpenAPI 3.1 JSON.
func Spec() []byte { return spec }

// Filter returns the description narrowed to the operations matching tag or path,
// with components.schemas pruned to the schemas those operations can actually
// reach.
//
// Pruning the schemas is the point. A trace record carries two dozen fields and
// references several other types, so a caller asking about logs and being handed
// every trace schema in the service has not been helped much. Reachability is
// computed transitively, because a kept schema's own fields reference others.
//
// An empty tag and path return the document unchanged. A filter matching nothing
// returns a document with no paths rather than an error: "no operation is tagged
// that way" is an answer, and one the caller can act on.
func Filter(raw []byte, tag, path string) ([]byte, error) {
	if tag == "" && path == "" {
		return raw, nil
	}

	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("openapi: parse spec: %w", err)
	}

	paths, _ := doc["paths"].(map[string]any)
	kept := make(map[string]any, len(paths))
	for route, item := range paths {
		if pruned, ok := keepPathItem(route, item, tag, path); ok {
			kept[route] = pruned
		}
	}
	doc["paths"] = kept

	if components, ok := doc["components"].(map[string]any); ok {
		if schemas, ok := components["schemas"].(map[string]any); ok {
			components["schemas"] = reachableSchemas(kept, schemas)
		}
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("openapi: encode filtered spec: %w", err)
	}
	return out, nil
}

// keepPathItem decides whether a path survives the filter, and with which
// operations. A path filter is a prefix match and keeps every method on the route;
// a tag filter drops the individual operations that do not carry the tag, so
// asking for "traces" on a route that also serves something else does not smuggle
// the something else back in.
func keepPathItem(route string, item any, tag, path string) (any, bool) {
	if path != "" {
		return item, strings.HasPrefix(route, path)
	}

	operations, ok := item.(map[string]any)
	if !ok {
		return nil, false
	}
	kept := make(map[string]any, len(operations))
	for method, op := range operations {
		if hasTag(op, tag) {
			kept[method] = op
		}
	}
	return kept, len(kept) > 0
}

// hasTag reports whether an operation carries tag.
func hasTag(op any, tag string) bool {
	operation, ok := op.(map[string]any)
	if !ok {
		return false
	}
	tags, ok := operation["tags"].([]any)
	if !ok {
		return false
	}
	for _, t := range tags {
		if name, ok := t.(string); ok && name == tag {
			return true
		}
	}
	return false
}

// reachableSchemas returns the subset of schemas referenced from paths, following
// references through the schemas themselves until the set stops growing.
func reachableSchemas(paths, schemas map[string]any) map[string]any {
	pending := collectRefs(paths)
	seen := make(map[string]struct{}, len(pending))
	out := make(map[string]any, len(pending))

	for len(pending) > 0 {
		name := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if _, done := seen[name]; done {
			continue
		}
		seen[name] = struct{}{}

		schema, ok := schemas[name]
		if !ok {
			// A dangling reference is the generator's problem, not ours: there is no
			// schema to carry, and the operation still references it either way. Skipping
			// is what leaves the filtered document exactly as broken as the input, rather
			// than failing a read because the input was.
			continue
		}
		out[name] = schema
		pending = append(pending, collectRefs(schema)...)
	}
	return out
}

// schemaRefPrefix is where the generator puts every component schema reference.
const schemaRefPrefix = "#/components/schemas/"

// collectRefs walks an arbitrary decoded JSON value and returns the component
// schema names it references. It looks at every "$ref" it finds at any depth
// rather than at the handful of places a reference is expected, because the
// generator nests schemas inside content types and array items — enumerating those
// shapes would be a list to keep in step with a tool we do not control.
func collectRefs(node any) []string {
	var found []string
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "$ref" {
				if ref, ok := child.(string); ok {
					if name, ok := strings.CutPrefix(ref, schemaRefPrefix); ok {
						found = append(found, name)
					}
				}
				continue
			}
			found = append(found, collectRefs(child)...)
		}
	case []any:
		for _, child := range value {
			found = append(found, collectRefs(child)...)
		}
	}
	return found
}
