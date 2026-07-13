package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// TestSetPayloadTemplateResource proves templateResource(id) is available in a
// set-payload expression and renders the referenced template against the message.
func TestSetPayloadTemplateResource(t *testing.T) {
	deps := core.BlockDeps{Resources: fakeTemplateLoader{"welcome.tmpl": []byte("Hi {{ body.name }}!")}}
	proc, err := newSetPayload(types.Settings{"value": `templateResource("welcome.tmpl")`}, deps)
	if err != nil {
		t.Fatalf("newSetPayload: %v", err)
	}
	out, err := proc.Process(context.Background(), newTestMessage(t, map[string]any{"name": "Bo"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.Body != "Hi Bo!" {
		t.Errorf("body = %v, want %q", out.Body, "Hi Bo!")
	}
}

// TestSetVariableTemplateResourceMissing surfaces a missing template as an
// evaluation error through the CEL function.
func TestSetVariableTemplateResourceMissing(t *testing.T) {
	deps := core.BlockDeps{Resources: fakeTemplateLoader{}}
	proc, err := newSetVariable(types.Settings{"name": "greeting", "value": `templateResource("nope.tmpl")`}, deps)
	if err != nil {
		t.Fatalf("newSetVariable: %v", err)
	}
	if _, err := proc.Process(context.Background(), newTestMessage(t, nil)); err == nil {
		t.Fatal("expected an error rendering a missing template")
	}
}
