package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
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

func TestParseAndRenderTemplate(t *testing.T) {
	tpl, err := parseTemplate("Hello {{ body.name }} — {{ 1 + 1 }}!")
	if err != nil {
		t.Fatalf("parseTemplate: %v", err)
	}
	act := map[string]any{"body": map[string]any{"name": "Ann"}}
	got, err := tpl.render(act)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if want := "Hello Ann — 2!"; got != want {
		t.Errorf("render = %q, want %q", got, want)
	}
}

func TestParseTemplateUnterminated(t *testing.T) {
	if _, err := parseTemplate("oops {{ body.x"); !errors.Is(err, errUnterminatedTemplate) {
		t.Fatalf("parseTemplate err = %v, want errUnterminatedTemplate", err)
	}
}

func TestParseTemplateBadExpression(t *testing.T) {
	if _, err := parseTemplate("{{ body..x }}"); err == nil {
		t.Fatal("expected a compile error for a malformed inner expression")
	}
}

func TestTemplateRegistryCachesAndErrors(t *testing.T) {
	reg := newTemplateRegistry(fakeTemplateLoader{"greeting": []byte("hi {{ body.name }}")})
	tpl1, err := reg.get(context.Background(), "greeting")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	tpl2, _ := reg.get(context.Background(), "greeting")
	if tpl1 != tpl2 {
		t.Error("registry did not cache the parsed template")
	}
	if _, err := reg.get(context.Background(), "missing"); err == nil {
		t.Error("expected an error for a missing template")
	}
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
	msg := newTestMessage(t, map[string]any{"name": "Bo"})
	out, err := proc.Process(context.Background(), msg)
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
