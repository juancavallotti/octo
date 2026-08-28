package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/internal/schemafmt"
)

// The default is YAML, verbatim, comments included: the prose is what an
// implementer is reading the document for.
func TestOpenAPIDocumentDefaultsToAnnotatedYAML(t *testing.T) {
	data, err := openapiDocument(schemafmt.FormatYAML)
	if err != nil {
		t.Fatalf("openapiDocument: %v", err)
	}
	if !strings.Contains(string(data), "# READ THIS FIRST") {
		t.Fatal("the YAML rendering lost the document's comments")
	}
	if !strings.Contains(string(data), "openapi: 3.1.0") {
		t.Fatal("the YAML rendering is not the contract")
	}
}

// JSON is for tooling that will not read YAML, so it has to be real JSON.
func TestOpenAPIDocumentAsJSON(t *testing.T) {
	data, err := openapiDocument(schemafmt.FormatJSON)
	if err != nil {
		t.Fatalf("openapiDocument: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("the JSON rendering does not parse: %v", err)
	}
	if doc["paths"] == nil {
		t.Fatal("the JSON rendering has no paths")
	}
}

func TestOpenAPIDocumentRejectsAnUnknownFormat(t *testing.T) {
	if _, err := openapiDocument("toml"); err == nil {
		t.Fatal("openapiDocument err = nil, want a failure naming the formats")
	}
}

// --out writes the contract where it was asked to, which is how the docs build
// takes its copy.
func TestOpenAPICommandWritesToAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "platform-api.yaml")
	if err := openapiCommand([]string{"--out", path}); err != nil {
		t.Fatalf("openapiCommand: %v", err)
	}
	written, err := os.ReadFile(path) //nolint:gosec // a path this test just made
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "openapi: 3.1.0") {
		t.Fatal("the written file is not the contract")
	}
}

// The command is in every build, not only the one with -tags api: a person
// deciding whether to implement this contract reads it from whatever octo they
// already have.
func TestOpenAPICommandIsAlwaysAvailable(t *testing.T) {
	if err := run([]string{"openapi", "--format", "json", "--out", filepath.Join(t.TempDir(), "s.json")}); err != nil {
		t.Fatalf("run openapi: %v", err)
	}
}
