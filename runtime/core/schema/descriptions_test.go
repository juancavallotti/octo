package schema

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// collectStructDocs is exercised with in-memory source (no fixture package or
// testdata needed). Backticks are legal inside a double-quoted Go string, so the
// struct tags embed directly.
const docSource = "package p\n" +
	"type Sample struct {\n" +
	"// Value is a CEL expression whose result replaces the message body.\n" +
	"Value string `json:\"value\"`\n" +
	"// RawBody stores the value as a raw-content body spread across two\n" +
	"// source lines so the overlay's line-joining is exercised.\n" +
	"RawBody bool `json:\"rawBody\"`\n" +
	"// Secret must be skipped because its json name is \"-\".\n" +
	"Secret string `json:\"-\"`\n" +
	"}\n" +
	"type Nested struct {\n" +
	"// Token authenticates the request.\n" +
	"Token string `json:\"token\"`\n" +
	"}\n"

func TestCollectStructDocs(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "sample.go", docSource, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := map[string]map[string]string{}
	collectStructDocs(file, out)

	if got := out["Sample"]["value"]; got != "Value is a CEL expression whose result replaces the message body." {
		t.Errorf("value doc = %q", got)
	}
	// A comment wrapped across two source lines is joined into one line.
	want := "RawBody stores the value as a raw-content body spread across two source lines so the overlay's line-joining is exercised."
	if got := out["Sample"]["rawBody"]; got != want {
		t.Errorf("rawBody doc = %q, want %q", got, want)
	}
	if _, ok := out["Sample"]["-"]; ok {
		t.Error(`a json:"-" field must not be recorded`)
	}
	if got := out["Nested"]["token"]; got != "Token authenticates the request." {
		t.Errorf("nested token doc = %q", got)
	}
}

// descOverlaySample and descOverlayInner are compiled so the overlay has real
// reflect types to walk; the doc map is supplied directly.
type descOverlaySample struct {
	Value  string           `json:"value" octo:"label=Value,type=cel"`
	Tagged string           `json:"tagged" octo:"label=Tagged,desc=from the tag"`
	Auth   descOverlayInner `json:"auth" octo:"label=Auth,type=object"`
	Plain  string           `json:"plain" octo:"label=Plain"`
}

type descOverlayInner struct {
	Token string `json:"token" octo:"label=Token"`
}

func TestOverlayFillsAndRespectsPrecedence(t *testing.T) {
	typ := reflect.TypeOf(descOverlaySample{})
	fields, err := fieldsOf(typ)
	if err != nil {
		t.Fatalf("fieldsOf: %v", err)
	}
	docs := map[string]map[string]string{
		"descOverlaySample": {"value": "Value desc", "tagged": "doc that must lose"},
		"descOverlayInner":  {"token": "Token desc"},
	}
	overlay(fields, typ, docs)

	if v := fieldByName(fields, "value"); v.Description != "Value desc" {
		t.Errorf("value description = %q", v.Description)
	}
	// An octo desc= clause wins over the doc map.
	if tg := fieldByName(fields, "tagged"); tg.Description != "from the tag" {
		t.Errorf("tagged description = %q, want %q", tg.Description, "from the tag")
	}
	// Nested object sub-fields are filled by recursion.
	if a := fieldByName(fields, "auth"); len(a.Fields) != 1 || a.Fields[0].Description != "Token desc" {
		t.Errorf("nested token description = %+v", a.Fields)
	}
	// A field with no doc-map entry keeps an empty description.
	if p := fieldByName(fields, "plain"); p.Description != "" {
		t.Errorf("plain description = %q, want empty", p.Description)
	}
}

func TestApplyDescriptionsMissingDirIsNoop(t *testing.T) {
	typ := reflect.TypeOf(descOverlaySample{})
	fields, _ := fieldsOf(typ)
	applyDescriptions(fields, "/no/such/dir", typ) // must not panic
	if v := fieldByName(fields, "value"); v.Description != "" {
		t.Errorf("expected no description without source, got %q", v.Description)
	}
}

func TestLoadPackageDocsReadsDirectory(t *testing.T) {
	// Exercise the os.ReadDir path against this package's own directory.
	_, file, _, _ := runtime.Caller(0)
	docs := loadPackageDocs(filepath.Dir(file))
	if docs == nil {
		t.Fatal("loadPackageDocs returned nil for a real package directory")
	}
}
