package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeTemplateLoader serves templates from an in-memory map.
type fakeTemplateLoader map[string][]byte

func (f fakeTemplateLoader) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	data, ok := f[id]
	if !ok {
		return nil, core.ErrResourceNotFound
	}
	return data, nil
}

func newTestMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatal(err)
	}
	msg.Body = body
	return msg
}

func TestTemplateResourceBlockBody(t *testing.T) {
	deps := core.BlockDeps{Resources: fakeTemplateLoader{"welcome.tmpl": []byte("Hi {{ body.name }}")}}
	proc, err := newTemplateResource(types.Settings{"id": "welcome.tmpl"}, deps)
	if err != nil {
		t.Fatalf("newTemplateResource: %v", err)
	}
	out, err := proc.Process(context.Background(), newTestMessage(t, map[string]any{"name": "Bo"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.Body != "Hi Bo" {
		t.Errorf("body = %v, want %q", out.Body, "Hi Bo")
	}
}

func TestTemplateResourceBlockTargetVariable(t *testing.T) {
	deps := core.BlockDeps{Resources: fakeTemplateLoader{"welcome.tmpl": []byte("Hi {{ body.name }}")}}
	proc, err := newTemplateResource(types.Settings{"id": "welcome.tmpl", "target": "greeting"}, deps)
	if err != nil {
		t.Fatalf("newTemplateResource: %v", err)
	}
	out, err := proc.Process(context.Background(), newTestMessage(t, map[string]any{"name": "Bo"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// Body is left untouched; the rendered text lands in the target variable.
	if _, isString := out.Body.(string); isString {
		t.Errorf("body should be untouched, got %v", out.Body)
	}
	if got, _ := out.Variables.String("greeting"); got != "Hi Bo" {
		t.Errorf("vars.greeting = %q, want %q", got, "Hi Bo")
	}
}

func TestTemplateResourceBlockRawBody(t *testing.T) {
	deps := core.BlockDeps{Resources: fakeTemplateLoader{"page.tmpl": []byte("<h1>Hi {{ body.name }}</h1>")}}
	proc, err := newTemplateResource(
		types.Settings{"id": "page.tmpl", "rawBody": true, "contentType": "text/html"},
		deps,
	)
	if err != nil {
		t.Fatalf("newTemplateResource: %v", err)
	}
	out, err := proc.Process(context.Background(), newTestMessage(t, map[string]any{"name": "Bo"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	ct, data, ok := out.RawBody()
	if !ok {
		t.Fatal("expected a raw-content body")
	}
	if ct != "text/html" || data != "<h1>Hi Bo</h1>" {
		t.Errorf("RawBody = (%q, %q), want (text/html, <h1>Hi Bo</h1>)", ct, data)
	}
}

func TestTemplateResourceBlockRawBodyValidation(t *testing.T) {
	deps := core.BlockDeps{Resources: fakeTemplateLoader{"page.tmpl": []byte("x")}}
	tests := []struct {
		name string
		raw  types.Settings
	}{
		{name: "rawBody without contentType", raw: types.Settings{"id": "page.tmpl", "rawBody": true}},
		{name: "rawBody with target", raw: types.Settings{"id": "page.tmpl", "rawBody": true, "contentType": "text/html", "target": "out"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newTemplateResource(tt.raw, deps); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestTemplateResourceBlockRequiresID(t *testing.T) {
	_, err := newTemplateResource(types.Settings{}, core.BlockDeps{})
	if err == nil || !strings.Contains(err.Error(), "id") {
		t.Fatalf("err = %v, want an id-required error", err)
	}
}

func TestTemplateResourceBlockMissingTemplate(t *testing.T) {
	deps := core.BlockDeps{Resources: fakeTemplateLoader{}}
	proc, err := newTemplateResource(types.Settings{"id": "nope.tmpl"}, deps)
	if err != nil {
		t.Fatalf("newTemplateResource: %v", err)
	}
	if _, err := proc.Process(context.Background(), newTestMessage(t, nil)); err == nil {
		t.Fatal("expected an error rendering a missing template")
	}
}
