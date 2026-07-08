package main

import (
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/schema"
)

// TestGeneratedSchemaIsWellFormed is a smoke test over the generated capability
// catalogue. It deliberately does not pin the schema to a golden fixture — the
// whole point of generating from Go is to retire the hand-maintained mirror, so
// duplicating it as testdata would defeat that. Instead it asserts the generator
// runs and the output satisfies the invariants the editor relies on. Every
// connector is blank-imported by main, so all metadata is registered here.
func TestGeneratedSchemaIsWellFormed(t *testing.T) {
	caps, err := schema.Generate(core.DefaultSchemaRegistry())
	if err != nil {
		t.Fatalf("generate schema: %v", err)
	}
	if len(caps.Blocks) == 0 {
		t.Fatal("no blocks generated")
	}
	if len(caps.Connectors) == 0 {
		t.Fatal("no connectors generated")
	}

	for _, b := range caps.Blocks {
		if b.Type == "" || b.Label == "" || b.Icon == "" {
			t.Errorf("block %q missing type/label/icon", b.Type)
		}
		if b.Category != "processor" && b.Category != "control-flow" {
			t.Errorf("block %q has unexpected category %q", b.Type, b.Category)
		}
		if len(b.Fields) == 0 {
			t.Errorf("block %q has no fields", b.Type)
		}
		checkFields(t, b.Type, b.Fields)
	}

	for _, c := range caps.Connectors {
		if c.Type == "" || c.Label == "" {
			t.Errorf("connector %q missing type/label", c.Type)
		}
		checkFields(t, c.Type, c.Settings)
		for _, s := range c.Sources {
			if s.Type == "" || s.Label == "" || s.Icon == "" {
				t.Errorf("connector %q source %q missing type/label/icon", c.Type, s.Type)
			}
			checkFields(t, c.Type+"/"+s.Type, s.Fields)
		}
	}

	// The generator must also marshal cleanly to the canonical bytes.
	if _, err := schema.Marshal(caps); err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
}

// checkFields asserts the per-field invariants the editor forms depend on: a
// label and type always, and a non-empty option list on every enum.
func checkFields(t *testing.T, owner string, fields []schema.Field) {
	t.Helper()
	for _, f := range fields {
		if f.Name == "" || f.Label == "" || f.Type == "" {
			t.Errorf("%s field %q missing name/label/type", owner, f.Name)
		}
		if f.Type == "enum" && len(f.Enum) == 0 {
			t.Errorf("%s enum field %q has no options", owner, f.Name)
		}
		checkFields(t, owner, f.Fields) // recurse into nested object fields
	}
}
