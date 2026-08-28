package openapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// document is the parsed contract, in the shape these tests read it.
type document struct {
	OpenAPI string `yaml:"openapi"`
	Info    struct {
		Version string `yaml:"version"`
	} `yaml:"info"`
	Paths      map[string]map[string]any `yaml:"paths"`
	Components struct {
		Schemas    map[string]any `yaml:"schemas"`
		Parameters map[string]any `yaml:"parameters"`
		Responses  map[string]any `yaml:"responses"`
		Headers    map[string]any `yaml:"headers"`
	} `yaml:"components"`
}

// parseSpec reads the embedded contract, failing the test if it will not parse.
func parseSpec(t *testing.T) document {
	t.Helper()
	var doc document
	if err := yaml.Unmarshal(Spec(), &doc); err != nil {
		t.Fatalf("the contract does not parse as YAML: %v", err)
	}
	return doc
}

func TestSpecIsOpenAPI31(t *testing.T) {
	doc := parseSpec(t)
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Fatalf("openapi = %q, want 3.1.x", doc.OpenAPI)
	}
	if len(doc.Paths) == 0 || len(doc.Components.Schemas) == 0 {
		t.Fatal("the contract has no paths or no schemas")
	}
}

// Every $ref has to resolve. A dangling one is invisible until somebody feeds the
// document to a generator, which is exactly the person we are publishing it for.
func TestEveryRefResolves(t *testing.T) {
	var whole any
	if err := yaml.Unmarshal(Spec(), &whole); err != nil {
		t.Fatal(err)
	}
	for _, ref := range collectRefs(whole) {
		if !strings.HasPrefix(ref, "#/") {
			t.Errorf("$ref %q is not a local reference; the contract must stand alone", ref)
			continue
		}
		if !resolves(whole, strings.Split(strings.TrimPrefix(ref, "#/"), "/")) {
			t.Errorf("$ref %q does not resolve", ref)
		}
	}
}

// collectRefs walks the document for every $ref value.
func collectRefs(node any) []string {
	var out []string
	switch v := node.(type) {
	case map[string]any:
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					out = append(out, ref)
				}
				continue
			}
			out = append(out, collectRefs(value)...)
		}
	case []any:
		for _, item := range v {
			out = append(out, collectRefs(item)...)
		}
	}
	return out
}

// resolves walks a JSON-pointer path through the document.
func resolves(node any, path []string) bool {
	for _, segment := range path {
		m, ok := node.(map[string]any)
		if !ok {
			return false
		}
		if node, ok = m[segment]; !ok {
			return false
		}
	}
	return true
}

// The version in the document and the constant the client compares against are
// one fact. Two copies of it would disagree the first time either moved.
func TestSpecVersionMatchesTheClientConstant(t *testing.T) {
	doc := parseSpec(t)
	if doc.Info.Version != ClientSpecVersion {
		t.Fatalf("info.version = %q but the client speaks %q", doc.Info.Version, ClientSpecVersion)
	}
}

// Nothing declared under components should be unused: a parameter or schema
// nobody references is either a leftover or a route somebody forgot to wire.
func TestNoOrphanedComponents(t *testing.T) {
	doc := parseSpec(t)
	var whole any
	if err := yaml.Unmarshal(Spec(), &whole); err != nil {
		t.Fatal(err)
	}
	referenced := map[string]bool{}
	for _, ref := range collectRefs(whole) {
		referenced[ref] = true
	}
	sections := map[string]map[string]any{
		"schemas":    doc.Components.Schemas,
		"parameters": doc.Components.Parameters,
		"responses":  doc.Components.Responses,
		"headers":    doc.Components.Headers,
	}
	for section, entries := range sections {
		for name := range entries {
			ref := fmt.Sprintf("#/components/%s/%s", section, name)
			if !referenced[ref] {
				t.Errorf("%s is declared and never referenced", ref)
			}
		}
	}
}

// The JSON rendering has to be real JSON, since it is what tooling that will not
// read YAML gets.
func TestSpecJSONParses(t *testing.T) {
	data, err := SpecJSON()
	if err != nil {
		t.Fatalf("SpecJSON: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("SpecJSON did not produce JSON: %v", err)
	}
	if doc["openapi"] == nil || doc["paths"] == nil {
		t.Fatal("the JSON rendering lost the document's structure")
	}
}

// The YAML rendering is byte-for-byte the file, comments included. Those comments
// are half the document — they say why a route is shaped the way it is — and an
// encoder round trip would drop every one.
func TestSpecKeepsItsComments(t *testing.T) {
	if !strings.Contains(string(Spec()), "# READ THIS FIRST") {
		t.Fatal("Spec() lost the document's comments; it must return the file verbatim")
	}
}
