package expr

import "testing"

func TestCompileSourcePayloadRendersTemplateResource(t *testing.T) {
	loader := fakeLoader{"greeting": []byte("every {{ settings.schedule }}")}
	prog, err := CompileSourcePayload(loader, `{"text": templateResource("greeting")}`)
	if err != nil {
		t.Fatalf("CompileSourcePayload: %v", err)
	}

	out, err := prog.Eval(SourcePayloadActivation(map[string]any{"schedule": "@every 2s"}))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	body, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("payload = %T, want a map", out)
	}
	if body["text"] != "every @every 2s" {
		t.Errorf("text = %v, want %q", body["text"], "every @every 2s")
	}
}

// A source has produced no message, so a template rendered from a payload sees the
// source's scope only — referencing a message variable must fail rather than
// silently render empty.
func TestCompileSourcePayloadRejectsMessageVars(t *testing.T) {
	if _, err := CompileSourcePayload(nil, `{"x": body.name}`); err == nil {
		t.Fatal("expected body to be undefined in a source payload")
	}
}

// The other registered extensions reach a payload through the same seam.
func TestCompileSourcePayloadHasJSONExtension(t *testing.T) {
	prog, err := CompileSourcePayload(nil, `toJson({"a": 1})`)
	if err != nil {
		t.Fatalf("CompileSourcePayload: %v", err)
	}
	if _, err := prog.EvalString(SourcePayloadActivation(nil)); err != nil {
		t.Fatalf("EvalString: %v", err)
	}
}
