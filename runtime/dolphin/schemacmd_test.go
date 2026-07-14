package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/dolphin/internal/suite"
)

// jsonSchema is as much of the schema document as the drift test needs to walk.
type jsonSchema struct {
	Type       string                `json:"type"`
	Properties map[string]jsonSchema `json:"properties"`
	Defs       map[string]jsonSchema `json:"$defs"`
	Ref        string                `json:"$ref"`
}

// loadSchema decodes the embedded schema.
func loadSchema(t *testing.T) jsonSchema {
	t.Helper()
	var doc jsonSchema
	if err := json.Unmarshal(testConfigSchema, &doc); err != nil {
		t.Fatalf("the embedded test-file schema is not valid JSON: %v", err)
	}
	return doc
}

// TestSchemaMatchesTheGoTypes is the drift guard.
//
// The schema is hand-written, so nothing but this stops it from quietly falling behind
// the format it describes. A field added to the Go types without a line here would be a
// field the editor cannot offer and a validator would reject — and, because the loader
// rejects unknown keys, a user following the schema would be told their valid suite is
// wrong. A field REMOVED from the types but left here is worse: the schema would invite
// people to write something the loader refuses.
//
// It walks the structs and the schema together, comparing property names at every level,
// so adding a field to either side without the other fails.
func TestSchemaMatchesTheGoTypes(t *testing.T) {
	doc := loadSchema(t)

	tests := []struct {
		name   string
		typ    reflect.Type
		schema jsonSchema
		drop   []string // fields the schema describes elsewhere, by design
	}{
		{name: "file", typ: reflect.TypeOf(suite.File{}), schema: doc},
		{name: "case", typ: reflect.TypeOf(suite.Case{}), schema: doc.Defs["case"]},
		{name: "input", typ: reflect.TypeOf(suite.Input{}), schema: doc.Defs["input"]},
		{name: "expect", typ: reflect.TypeOf(suite.Expectation{}), schema: doc.Defs["expect"]},
		{name: "messageExpect", typ: reflect.TypeOf(suite.MessageExpect{}), schema: doc.Defs["messageExpect"]},
		{name: "spyExpect", typ: reflect.TypeOf(suite.SpyExpect{}), schema: doc.Defs["spyExpect"]},
		{name: "recordExpect", typ: reflect.TypeOf(suite.RecordExpect{}), schema: doc.Defs["recordExpect"]},
		// The mocks are the engine's own types, not dolphin's — a suite runs the same
		// mock the debug seam does — so the schema has to track core, not suite.
		{name: "mockSpec", typ: reflect.TypeOf(core.MockSpec{}), schema: doc.Defs["mockSpec"]},
		{name: "mockCase", typ: reflect.TypeOf(core.MockCase{}), schema: doc.Defs["mockCase"]},
		// The default is a case with no `when`, so it is described separately — but it
		// must still cover every other field of the same Go type.
		{name: "defaultCase", typ: reflect.TypeOf(core.MockCase{}), schema: doc.Defs["defaultCase"], drop: []string{"when"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			want := remove(yamlFields(tc.typ), tc.drop...)
			got := keys(tc.schema.Properties)

			if diff := missing(want, got); len(diff) > 0 {
				t.Errorf("the schema does not describe %v — a field was added to %s without it", diff, tc.typ)
			}
			if diff := missing(got, want); len(diff) > 0 {
				t.Errorf("the schema describes %v, which %s does not have — a field was removed or renamed", diff, tc.typ)
			}
		})
	}
}

// TestExpectationInlinesTheMessageMatcher: `expect` embeds MessageExpect inline, so its
// fields (body, vars, that) are the expectation's own keys in YAML. yamlFields cannot see
// through an embedded struct, so the schema is checked against the flattened set the user
// actually writes — and this pins that they stay flattened.
func TestExpectationInlinesTheMessageMatcher(t *testing.T) {
	expect := loadSchema(t).Defs["expect"]

	for _, field := range yamlFields(reflect.TypeOf(suite.MessageExpect{})) {
		if _, ok := expect.Properties[field]; !ok {
			t.Errorf("`expect` does not describe %q — the inline message matcher was lost", field)
		}
	}
	for _, field := range []string{"dropped", "error"} {
		if _, ok := expect.Properties[field]; !ok {
			t.Errorf("`expect` does not describe %q", field)
		}
	}
}

// TestSchemaAcceptsTheDocumentedExample: the suite in the docs must be one the loader
// takes. A schema describing a shape the loader rejects is worse than none.
func TestSchemaAcceptsTheDocumentedExample(t *testing.T) {
	path := write(t, "orders_test.yaml", documentedSuite)

	file, err := suite.Load(path)
	if err != nil {
		t.Fatalf("the documented example does not load: %v", err)
	}
	if len(file.Cases) < 3 {
		t.Fatal("the example no longer exercises enough of the format to be worth pinning")
	}

	doc := loadSchema(t)
	for _, section := range []string{"flow", "inputs", "mocks", "cases"} {
		if _, ok := doc.Properties[section]; !ok {
			t.Errorf("the schema has no %q section, but the example uses it", section)
		}
	}
}

// documentedSuite is the example from the docs, and the one the schema is written for.
const documentedSuite = `
flow: charge-flowlevel

inputs:
  small:
    data: { amount: 10 }
  large:
    data: { amount: 500 }
    vars: { x-api-key: dev-key }

mocks:
  charge-flowlevel.call-charge:
    cases:
      - when: 'body.amount > 100'
        error: card declined
    default:
      body: { id: ch_1, status: captured }

timeout: 10s

cases:
  - name: a small charge is captured
    input: small
    expect:
      body: { id: ch_1, status: captured }

  - name: a declined card takes the flow's error path
    input: large
    expect:
      vars: { httpStatus: "502" }
      that:
        - 'body.error.contains("declined")'
    spies:
      charge-flowlevel.call-charge:
        count: 1
        records:
          - input:
              that: ['body.amount == 500']
            error: card declined

  - name: it is filtered out
    input:
      data: { amount: 0 }
    mocks:
      charge-flowlevel.call-charge:
        cases:
          - when: 'true'
            drop: true
    expect:
      dropped: true

  - name: not ready yet
    skip: the upstream contract is still moving
`

// write puts content in a temp file and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// yamlFields returns the yaml names of a struct's fields, flattening embedded ones — an
// inline struct's fields are keys of the parent in YAML, which is what a user writes and
// so what the schema must describe.
func yamlFields(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		field := t.Field(i)
		tag := field.Tag.Get("yaml")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if field.Anonymous && strings.Contains(tag, "inline") {
			names = append(names, yamlFields(field.Type)...)
			continue
		}
		names = append(names, name)
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

func remove(names []string, drop ...string) []string {
	gone := make(map[string]bool, len(drop))
	for _, name := range drop {
		gone[name] = true
	}
	kept := make([]string, 0, len(names))
	for _, name := range names {
		if !gone[name] {
			kept = append(kept, name)
		}
	}
	return kept
}
