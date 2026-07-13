package runtime

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeTemplateLoader serves template resources from an in-memory map keyed by id.
type fakeTemplateLoader map[string][]byte

func (f fakeTemplateLoader) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	data, ok := f[id]
	if !ok {
		return nil, core.ErrResourceNotFound
	}
	return data, nil
}

func TestWithTemplateAliasesResolvesAlias(t *testing.T) {
	base := fakeTemplateLoader{"templates/welcome.tmpl": []byte("hi")}
	loader := withTemplateAliases(base, []types.TemplateResource{{Resource: "templates/welcome.tmpl", As: "welcome"}})

	data, err := loader.Load(context.Background(), core.ResourceKindTemplate, "welcome")
	if err != nil {
		t.Fatalf("load by alias: %v", err)
	}
	if string(data) != "hi" {
		t.Errorf("alias load = %q, want %q", data, "hi")
	}
}

func TestWithTemplateAliasesPassesThroughDirectID(t *testing.T) {
	base := fakeTemplateLoader{"templates/welcome.tmpl": []byte("hi")}
	loader := withTemplateAliases(base, []types.TemplateResource{{Resource: "templates/welcome.tmpl", As: "welcome"}})

	// An id that is not a declared alias is loaded directly.
	if _, err := loader.Load(context.Background(), core.ResourceKindTemplate, "templates/welcome.tmpl"); err != nil {
		t.Fatalf("direct-id load: %v", err)
	}
}

func TestWithTemplateAliasesNoAliasesReturnsBase(t *testing.T) {
	base := fakeTemplateLoader{}
	// No entries carry an alias, so the base loader is returned unchanged.
	loader := withTemplateAliases(base, []types.TemplateResource{{Resource: "templates/plain.tmpl"}})
	if _, ok := loader.(fakeTemplateLoader); !ok {
		t.Errorf("expected the base loader unwrapped, got %T", loader)
	}
}

func TestWarmTemplatesMissingFailsFast(t *testing.T) {
	err := warmTemplates(fakeTemplateLoader{}, []types.TemplateResource{{Resource: "templates/nope.tmpl", As: "nope"}})
	if err == nil {
		t.Fatal("expected warmTemplates to fail for a missing declared template")
	}
}

func TestWarmTemplatesMalformedFailsFast(t *testing.T) {
	base := fakeTemplateLoader{"bad.tmpl": []byte("hello {{ body.name ")} // unterminated {{
	if err := warmTemplates(base, []types.TemplateResource{{Resource: "bad.tmpl"}}); err == nil {
		t.Fatal("expected warmTemplates to fail for a malformed template")
	}
}

func TestWarmTemplatesValidPasses(t *testing.T) {
	base := fakeTemplateLoader{"ok.tmpl": []byte("hi {{ body.name }}")}
	if err := warmTemplates(base, []types.TemplateResource{{Resource: "ok.tmpl", As: "ok"}}); err != nil {
		t.Fatalf("warmTemplates on a valid template: %v", err)
	}
}
