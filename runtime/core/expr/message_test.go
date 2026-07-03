package expr

import (
	"testing"
	"time"
)

func messageActivation(body any) map[string]any {
	return map[string]any{
		"body":          body,
		"vars":          map[string]any{},
		"eventID":       "evt",
		"correlationID": "corr",
		"env":           map[string]any{"GREETING": "hi"},
		"now":           time.Unix(0, 0),
	}
}

func TestCompileMessageRendersTemplateResource(t *testing.T) {
	loader := fakeLoader{"welcome": []byte("{{ env.GREETING }} {{ body.name }}")}
	prog, err := CompileMessage(loader, `templateResource("welcome")`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.EvalString(messageActivation(map[string]any{"name": "Ann"}))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	if out != "hi Ann" {
		t.Errorf("render = %q, want %q", out, "hi Ann")
	}
}

func TestCompileMessageTemplateResourceComposable(t *testing.T) {
	loader := fakeLoader{"x": []byte("world")}
	prog, err := CompileMessage(loader, `"hello " + templateResource("x")`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.EvalString(messageActivation(nil))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	if out != "hello world" {
		t.Errorf("render = %q, want %q", out, "hello world")
	}
}

func TestCompileMessageMissingTemplate(t *testing.T) {
	prog, err := CompileMessage(fakeLoader{}, `templateResource("nope")`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	if _, err := prog.EvalString(messageActivation(nil)); err == nil {
		t.Fatal("expected a missing-template evaluation error")
	}
}

// Plain Compile does not expose templateResource — it is a message extension.
func TestPlainCompileRejectsTemplateResource(t *testing.T) {
	if _, err := Compile(`templateResource("x")`, MessageVars...); err == nil {
		t.Fatal("expected templateResource to be undefined for plain Compile")
	}
}
