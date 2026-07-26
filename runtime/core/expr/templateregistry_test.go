package expr

import (
	"context"
	"errors"
	"strings"
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
	tpl, err := ParseTemplate("Hello {{ body.name }} — {{ 1 + 1 }}!", nil)
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
	if _, err := ParseTemplate("oops {{ body.x", nil); !errors.Is(err, ErrUnterminatedTemplate) {
		t.Fatalf("ParseTemplate err = %v, want ErrUnterminatedTemplate", err)
	}
}

func TestParseTemplateBadExpression(t *testing.T) {
	if _, err := ParseTemplate("{{ body..x }}", nil); err == nil {
		t.Fatal("expected a compile error for a malformed inner expression")
	}
}

// TestTemplateSpansHaveEveryExtension is the drift guard: a template span and a
// message expression are the same language, so they must expose the same
// functions. Template spans used to compile through the bare Compile, which
// declares variables and nothing else, so every custom function was undefined
// inside {{ }} — `undeclared reference to 'toJson'` — while working everywhere
// else. Each case must compile and render in both environments; a function
// registered later that reaches only one of them fails here.
func TestTemplateSpansHaveEveryExtension(t *testing.T) {
	loader := fakeLoader{"greeting": []byte("hi")}
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"toJson", `toJson(body)`, `{"a":1}`},
		{"fromJson", `fromJson('{"n":2}').n`, `2`},
		{"toFormData", `toFormData(body)`, `a=1`},
		{"fromFormData", `fromFormData("a=1").a`, `1`},
		{"templateResource", `templateResource("greeting")`, `hi`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			act := messageActivation(map[string]any{"a": 1})

			tpl, err := ParseTemplate("{{ "+tc.expr+" }}", loader)
			if err != nil {
				t.Fatalf("ParseTemplate: %v", err)
			}
			got, err := tpl.Render(act)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if got != tc.want {
				t.Errorf("template render = %q, want %q", got, tc.want)
			}

			prog, err := CompileMessage(loader, tc.expr)
			if err != nil {
				t.Fatalf("CompileMessage: %v", err)
			}
			msg, err := prog.EvalString(act)
			if err != nil {
				t.Fatalf("EvalString: %v", err)
			}
			if msg != got {
				t.Errorf("message expression = %q, template = %q; the two must agree", msg, got)
			}
		})
	}
}

// TestTemplateComposesTemplate covers the sharper edge of the same gap: a
// template could not reach templateResource, so templates could not compose.
func TestTemplateComposesTemplate(t *testing.T) {
	reg := NewTemplateRegistry(fakeLoader{
		"page":     []byte(`<h1>{{ templateResource("greeting") }}</h1>`),
		"greeting": []byte("hi {{ body.name }}"),
	})
	tpl, err := reg.Get(context.Background(), "page")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := tpl.Render(messageActivation(map[string]any{"name": "Ann"}))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "<h1>hi Ann</h1>"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}
}

// TestTemplateRecursionIsBounded pins the guard that composition needs: the id is
// an expression argument, so a cycle cannot be caught at parse time, and without
// a depth bound a self-referencing template renders until the stack gives out.
func TestTemplateRecursionIsBounded(t *testing.T) {
	reg := NewTemplateRegistry(fakeLoader{"loop": []byte(`{{ templateResource("loop") }}`)})
	tpl, err := reg.Get(context.Background(), "loop")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	_, err = tpl.Render(messageActivation(nil))
	if err == nil {
		t.Fatal("expected a self-referencing template to be refused, not rendered")
	}
	if !strings.Contains(err.Error(), "recursive template") {
		t.Errorf("Render err = %v, want the depth guard", err)
	}
}

// TestTemplateRendersOutsideItsScope covers the cost of padding the activation:
// a template is compiled for the union of every scope it might be rendered from,
// so a message render has no "settings". That must not break a span that never
// mentions it — including one calling templateResource, whose macro names every
// variable in scope.
func TestTemplateRendersOutsideItsScope(t *testing.T) {
	reg := NewTemplateRegistry(fakeLoader{
		"page":     []byte(`{{ templateResource("greeting") }}|{{ settings }}`),
		"greeting": []byte("hi"),
	})
	tpl, err := reg.Get(context.Background(), "page")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got, err := tpl.Render(messageActivation(nil))
	if err != nil {
		t.Fatalf("Render from a message scope: %v", err)
	}
	if want := "hi|null"; got != want {
		t.Errorf("Render = %q, want %q", got, want)
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
