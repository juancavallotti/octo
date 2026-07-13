package expr

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// fakeLoader serves resources from an in-memory map; an absent id is not found.
type fakeLoader map[string][]byte

func (f fakeLoader) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	data, ok := f[id]
	if !ok {
		return nil, core.ErrResourceNotFound
	}
	return data, nil
}

func TestParseAndRenderTemplate(t *testing.T) {
	tpl, err := ParseTemplate("Hello {{ body.name }} — {{ 1 + 1 }}!")
	if err != nil {
		t.Fatalf("ParseTemplate: %v", err)
	}
	got, err := tpl.Render(map[string]any{"body": map[string]any{"name": "Ann"}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "Hello Ann — 2!"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

func TestParseTemplateUnterminated(t *testing.T) {
	if _, err := ParseTemplate("oops {{ body.x"); !errors.Is(err, ErrUnterminatedTemplate) {
		t.Fatalf("ParseTemplate err = %v, want ErrUnterminatedTemplate", err)
	}
}

func TestParseTemplateBadExpression(t *testing.T) {
	if _, err := ParseTemplate("{{ body..x }}"); err == nil {
		t.Fatal("expected a compile error for a malformed inner expression")
	}
}

func TestTemplateRegistryCachesAndErrors(t *testing.T) {
	reg := NewTemplateRegistry(fakeLoader{"greeting": []byte("hi {{ body.name }}")})
	tpl1, err := reg.Get(context.Background(), "greeting")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	tpl2, _ := reg.Get(context.Background(), "greeting")
	if tpl1 != tpl2 {
		t.Error("registry did not cache the parsed template")
	}
	if _, err := reg.Get(context.Background(), "missing"); err == nil {
		t.Error("expected an error for a missing template")
	}
}
