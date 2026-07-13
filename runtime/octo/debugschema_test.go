package main

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/juancavallotti/octo/runtime/core"
)

// jsonSchema is as much of the schema document as the drift test needs to walk.
type jsonSchema struct {
	Type                 string                `json:"type"`
	Properties           map[string]jsonSchema `json:"properties"`
	AdditionalProperties json.RawMessage       `json:"additionalProperties"`
	Defs                 map[string]jsonSchema `json:"$defs"`
	Ref                  string                `json:"$ref"`
}

// loadDebugSchema decodes the embedded schema.
func loadDebugSchema(t *testing.T) jsonSchema {
	t.Helper()
	var doc jsonSchema
	if err := json.Unmarshal(debugConfigSchema, &doc); err != nil {
		t.Fatalf("the embedded debug-config schema is not valid JSON: %v", err)
	}
	return doc
}

// TestDebugConfigSchemaMatchesTheGoTypes is the drift guard.
//
// The schema is hand-written, so nothing but this test stops it from quietly falling
// behind the types it describes. A field added to debugConfig or core.MockCase without
// a line here would be a field the editor cannot offer and a validator would reject —
// and, because the loader rejects unknown keys, a user following the schema would be
// told their valid config is wrong.
//
// It walks the Go structs and the schema together, comparing the property names at
// every level, so adding a field to either side without the other fails.
func TestDebugConfigSchemaMatchesTheGoTypes(t *testing.T) {
	doc := loadDebugSchema(t)

	tests := []struct {
		name   string
		typ    reflect.Type
		schema jsonSchema
	}{
		{name: "debugConfig", typ: reflect.TypeOf(debugConfig{}), schema: doc},
		{name: "input", typ: reflect.TypeOf(debugInput{}), schema: doc.Defs["input"]},
		{name: "mockSpec", typ: reflect.TypeOf(core.MockSpec{}), schema: doc.Defs["mockSpec"]},
		{name: "mockCase", typ: reflect.TypeOf(core.MockCase{}), schema: doc.Defs["mockCase"]},
		// The default is a case with no `when`, so it is described separately — but it
		// must still cover every other field of the same Go type.
		{name: "defaultCase", typ: reflect.TypeOf(core.MockCase{}), schema: doc.Defs["defaultCase"]},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := yamlFields(tc.typ)
			if tc.name == "defaultCase" {
				want = remove(want, "when")
			}
			got := keys(tc.schema.Properties)

			if diff := missing(want, got); len(diff) > 0 {
				t.Errorf("the schema does not describe %v — a field was added to %s without it",
					diff, tc.typ)
			}
			if diff := missing(got, want); len(diff) > 0 {
				t.Errorf("the schema describes %v, which %s does not have — a field was removed or renamed",
					diff, tc.typ)
			}
		})
	}
}

// TestDebugConfigSchemaAcceptsTheDocumentedExample: the example in the usage text and
// the docs must be a config the loader takes. A schema that describes a shape the
// loader rejects is worse than none.
func TestDebugConfigSchemaAcceptsTheDocumentedExample(t *testing.T) {
	cfg, err := loadDebugConfig(writeConfig(t, "documented.yaml", sessionYAML))
	if err != nil {
		t.Fatalf("the documented example does not load: %v", err)
	}

	doc := loadDebugSchema(t)
	for _, section := range []string{"input", "breakAt", "spies", "mocks"} {
		if _, ok := doc.Properties[section]; !ok {
			t.Errorf("the schema has no %q section, but the example uses it", section)
		}
	}
	if cfg.BreakAt == "" || len(cfg.Spies) == 0 || len(cfg.Mocks) == 0 {
		t.Fatal("the example no longer exercises every section")
	}
}

func TestSchemaDocumentKinds(t *testing.T) {
	t.Run("debug-config is the embedded schema", func(t *testing.T) {
		data, err := schemaDocument(kindDebugConfig)
		if err != nil {
			t.Fatalf("schemaDocument: %v", err)
		}
		if !json.Valid(data) {
			t.Error("the debug-config schema is not valid JSON")
		}
	})

	t.Run("capabilities is still the default and still generates", func(t *testing.T) {
		data, err := schemaDocument(kindCapabilities)
		if err != nil {
			t.Fatalf("schemaDocument: %v", err)
		}
		if !json.Valid(data) {
			t.Error("the capability catalogue is not valid JSON")
		}
	})

	t.Run("an unknown kind is rejected", func(t *testing.T) {
		_, err := schemaDocument("nope")
		if err == nil {
			t.Fatal("an unknown kind must be rejected")
		}
		if !strings.Contains(err.Error(), kindDebugConfig) {
			t.Errorf("error = %q, want it to list the kinds that exist", err)
		}
	})
}

// TestFormatSchemaYAMLKeepsKeyOrder: a schema is read top to bottom, so `type` and
// `description` belong above the properties they describe. Going through a map would
// alphabetize them; the yaml.Node path keeps the order the document was written in.
func TestFormatSchemaYAMLKeepsKeyOrder(t *testing.T) {
	source := []byte(
		`{"type":"object","description":"d","properties":{"zebra":{"type":"string"},"apple":{"type":"string"}}}`)

	encoded, err := formatSchema(source, formatYAML)
	if err != nil {
		t.Fatalf("formatSchema: %v", err)
	}

	// It is YAML, and it round-trips to the same document.
	var got map[string]any
	if err := yaml.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("the yaml output does not parse: %v", err)
	}
	if got["type"] != "object" {
		t.Errorf("yaml lost a field: %v", got)
	}

	text := string(encoded)
	if indexOf(text, "type:") > indexOf(text, "properties:") {
		t.Errorf("the yaml was reordered — `type` must still come before `properties`:\n%s", text)
	}
	if indexOf(text, "zebra:") > indexOf(text, "apple:") {
		t.Errorf("the properties were alphabetized, losing the document's order:\n%s", text)
	}

	// Actually YAML, not the JSON it came from. Parsing JSON as YAML records every
	// mapping as flow-style, and Marshal honors that — so without stripping the style
	// the "yaml" output is one long braced line, which is just the JSON again.
	if strings.Contains(text, `"type": "object"`) {
		t.Errorf("the yaml output is still in JSON's flow style:\n%s", text)
	}
	if !strings.Contains(text, "\nproperties:\n") || !strings.Contains(text, "zebra:\n") {
		t.Errorf("the yaml output is not indented block style:\n%s", text)
	}
}

func TestFormatSchemaRejectsAnUnknownFormat(t *testing.T) {
	if _, err := formatSchema([]byte(`{}`), "toml"); err == nil {
		t.Fatal("an unknown format must be rejected")
	}
}

// yamlFields returns the yaml names of a struct's fields.
func yamlFields(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		names = append(names, strings.Split(tag, ",")[0])
	}
	sort.Strings(names)
	return names
}

func keys(m map[string]jsonSchema) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// missing returns the entries of want that are not in got.
func missing(want, got []string) []string {
	have := make(map[string]bool, len(got))
	for _, name := range got {
		have[name] = true
	}
	var diff []string
	for _, name := range want {
		if !have[name] {
			diff = append(diff, name)
		}
	}
	return diff
}

func remove(names []string, drop string) []string {
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if name != drop {
			kept = append(kept, name)
		}
	}
	return kept
}

func indexOf(haystack, needle string) int { return strings.Index(haystack, needle) }
