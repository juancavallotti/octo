package api

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/services/api/openapi"
	"gopkg.in/yaml.v3"
)

// The drift gate.
//
// The orchestrator's OpenAPI document is generated from its handlers, so it
// cannot describe a route the code does not serve. This one is hand-written for
// an API this repo never implements, so nothing stops it drifting from the client
// — except these tests. They are the same move services/k8s already makes for the
// Redis wire contract, and they fail saying WHAT drifted rather than that
// something did.
//
// They live here rather than beside the document because the check needs the
// route table, and the openapi package cannot import this one without a cycle.

// specDocument is the contract in the shape these tests read it.
type specDocument struct {
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas map[string]specSchema `yaml:"schemas"`
	} `yaml:"components"`
}

type specSchema struct {
	Properties map[string]any `yaml:"properties"`
	AllOf      []specSchema   `yaml:"allOf"`
	Ref        string         `yaml:"$ref"`
}

func parseSpecDocument(t *testing.T) specDocument {
	t.Helper()
	var doc specDocument
	if err := yaml.Unmarshal(openapi.Spec(), &doc); err != nil {
		t.Fatalf("the published contract does not parse: %v", err)
	}
	return doc
}

// Every route the module calls is documented, and every documented route is one
// the module calls. Either direction failing is a lie told to an implementer:
// one asks them to implement something nobody calls, the other calls something
// they were never told about.
func TestSpecDocumentsExactlyTheRoutesWeCall(t *testing.T) {
	doc := parseSpecDocument(t)

	documented := map[string]bool{}
	for path, operations := range doc.Paths {
		for method := range operations {
			switch strings.ToLower(method) {
			case "get", "put", "post", "delete", "patch", "head", "options":
				documented[strings.ToUpper(method)+" "+path] = true
			}
		}
	}

	called := map[string]bool{}
	for _, r := range routes {
		called[r.method+" "+r.path] = true
	}

	for key := range called {
		if !documented[key] {
			t.Errorf("the module calls %s and the contract does not document it — "+
				"an implementer would never know to write it", key)
		}
	}
	for key := range documented {
		if !called[key] {
			t.Errorf("the contract documents %s and the module never calls it — "+
				"an implementer would write it for nothing", key)
		}
	}
}

// The discovery document is what an implementer fills in, so every field the Go
// struct reads has to be described and nothing described may be missing from the
// struct. Adding a knob in Go without adding it here fails right here.
func TestDiscoverySchemaMatchesTheGoStructs(t *testing.T) {
	doc := parseSpecDocument(t)

	cases := []struct {
		schema string
		value  any
	}{
		{"Discovery", discoveryDocument{}},
		{"Implementation", implementation{}},
		{"Features", featureDocument{}},
		{"FeatureFlags", featureFlags{}},
		{"KVFeature", kvFeature{}},
		{"SecretsFeature", secretsFeature{}},
		{"LeaseFeature", leaseFeature{}},
		{"LeaderFeature", leaderFeature{}},
		{"QueueFeature", queueFeature{}},
		{"TopicFeature", topicFeature{}},
		{"AgentMemoryFeature", agentMemoryFeature{}},
		{"TraceFeature", traceFeature{}},
	}
	for _, tc := range cases {
		t.Run(tc.schema, func(t *testing.T) {
			want := jsonFieldNames(reflect.TypeOf(tc.value))
			got := schemaProperties(t, doc, tc.schema)
			assertSameFields(t, tc.schema, want, got)
		})
	}
}

// The message envelope is the other schema an implementer stores and returns
// whole, so it gets the same treatment.
func TestMessageSchemaMatchesTheGoStruct(t *testing.T) {
	doc := parseSpecDocument(t)
	assertSameFields(t, "Message",
		jsonFieldNames(reflect.TypeOf(messageWire{})),
		schemaProperties(t, doc, "Message"))
}

// assertSameFields compares two field-name sets and reports each difference in
// the direction that says what to do about it.
func assertSameFields(t *testing.T, schema string, want, got []string) {
	t.Helper()
	inSpec := map[string]bool{}
	for _, name := range got {
		inSpec[name] = true
	}
	inGo := map[string]bool{}
	for _, name := range want {
		inGo[name] = true
	}
	for _, name := range want {
		if !inSpec[name] {
			t.Errorf("%s.%s is read by the runtime and undocumented — an implementer "+
				"cannot know to send it", schema, name)
		}
	}
	for _, name := range got {
		if !inGo[name] {
			t.Errorf("%s.%s is documented and the runtime never reads it — an "+
				"implementer would send it for nothing", schema, name)
		}
	}
}

// schemaProperties returns a schema's property names, following allOf one level
// so the shared FeatureFlags fields count as the feature's own.
func schemaProperties(t *testing.T, doc specDocument, name string) []string {
	t.Helper()
	schema, ok := doc.Components.Schemas[name]
	if !ok {
		t.Fatalf("the contract has no %s schema", name)
	}
	names := propertyNames(schema)
	for _, part := range schema.AllOf {
		if part.Ref != "" {
			names = append(names, propertyNames(doc.Components.Schemas[refName(part.Ref)])...)
			continue
		}
		names = append(names, propertyNames(part)...)
	}
	sort.Strings(names)
	return names
}

func propertyNames(schema specSchema) []string {
	out := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		out = append(out, name)
	}
	return out
}

// refName reads the last segment of a local $ref.
func refName(ref string) string {
	parts := strings.Split(ref, "/")
	return parts[len(parts)-1]
}

// jsonFieldNames returns the JSON names a struct serializes to, flattening
// embedded structs the way encoding/json does.
func jsonFieldNames(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		field := t.Field(i)
		tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		if field.Anonymous && tag == "" {
			out = append(out, jsonFieldNames(field.Type)...)
			continue
		}
		if tag == "" {
			tag = field.Name
		}
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}
