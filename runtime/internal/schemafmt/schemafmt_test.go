package schemafmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestFormatYAMLKeepsKeyOrder: a schema is read top to bottom, so `type` and
// `description` belong above the properties they describe. Going through a map would
// alphabetize them; the yaml.Node path keeps the order the document was written in.
func TestFormatYAMLKeepsKeyOrder(t *testing.T) {
	source := []byte(
		`{"type":"object","description":"d","properties":{"zebra":{"type":"string"},"apple":{"type":"string"}}}`)

	encoded, err := Format(source, FormatYAML)
	if err != nil {
		t.Fatalf("Format: %v", err)
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
	if strings.Index(text, "type:") > strings.Index(text, "properties:") {
		t.Errorf("the yaml was reordered — `type` must still come before `properties`:\n%s", text)
	}
	if strings.Index(text, "zebra:") > strings.Index(text, "apple:") {
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

// TestFormatJSONIsTheDocumentItself: JSON is what the documents already are, so
// formatting one must not reflow or re-key it.
func TestFormatJSONIsTheDocumentItself(t *testing.T) {
	source := []byte("{\"b\":1,\"a\":2}\n")

	got, err := Format(source, FormatJSON)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if string(got) != string(source) {
		t.Errorf("Format(json) = %q, want the document unchanged", got)
	}
}

func TestFormatRejectsAnUnknownFormat(t *testing.T) {
	if _, err := Format([]byte(`{}`), "toml"); err == nil {
		t.Fatal("an unknown format must be rejected")
	}
}

// TestWriteToAFile: --out writes the rendered document, not the raw JSON.
func TestWriteToAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.yaml")

	if err := Write([]byte(`{"type":"object"}`), FormatYAML, path); err != nil {
		t.Fatalf("Write: %v", err)
	}

	written, err := os.ReadFile(path) //nolint:gosec // G304: a path this test just built under t.TempDir()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(string(written), "type: object") {
		t.Errorf("wrote %q, want the YAML rendering", written)
	}
}

// TestWriteRejectsABadFormatBeforeTouchingTheFile: a bad --format must not leave a
// truncated file behind.
func TestWriteRejectsABadFormatBeforeTouchingTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schema.toml")

	if err := Write([]byte(`{}`), "toml", path); err == nil {
		t.Fatal("an unknown format must be rejected")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a rejected format still created the file")
	}
}
