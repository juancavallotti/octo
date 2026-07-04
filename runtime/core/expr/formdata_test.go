package expr

import (
	"reflect"
	"testing"
)

func TestToFormData(t *testing.T) {
	prog, err := CompileMessage(nil, `toFormData(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.EvalString(messageActivation(map[string]any{"name": "Ann Lee", "count": 2}))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	// Encode sorts keys and escapes values, so this is exact.
	if out != "count=2&name=Ann+Lee" {
		t.Errorf("toFormData = %q, want %q", out, "count=2&name=Ann+Lee")
	}
}

func TestToFormDataRepeatedKey(t *testing.T) {
	prog, err := CompileMessage(nil, `toFormData(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.EvalString(messageActivation(map[string]any{"tag": []any{"a", "b"}}))
	if err != nil {
		t.Fatalf("EvalString: %v", err)
	}
	if out != "tag=a&tag=b" {
		t.Errorf("toFormData = %q, want %q", out, "tag=a&tag=b")
	}
}

func TestFromFormData(t *testing.T) {
	prog, err := CompileMessage(nil, `fromFormData(body).name`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.Eval(messageActivation("name=Ann&count=2"))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if out != "Ann" {
		t.Errorf("fromFormData(body).name = %v, want Ann", out)
	}
}

func TestFromFormDataRepeatedKey(t *testing.T) {
	prog, err := CompileMessage(nil, `fromFormData(body)`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.Eval(messageActivation("tag=a&tag=b"))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	got, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("result is %T, want map", out)
	}
	if !reflect.DeepEqual(got["tag"], []any{"a", "b"}) {
		t.Errorf("tag = %#v, want [a b]", got["tag"])
	}
}

func TestFormDataRoundTrip(t *testing.T) {
	prog, err := CompileMessage(nil, `fromFormData(toFormData(body))`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	out, err := prog.Eval(messageActivation(map[string]any{"name": "Ann Lee", "city": "berlin"}))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	want := map[string]any{"name": "Ann Lee", "city": "berlin"}
	if !reflect.DeepEqual(out, want) {
		t.Errorf("round-trip = %#v, want %#v", out, want)
	}
}
