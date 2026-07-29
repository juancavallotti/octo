package pinecone

import (
	"testing"

	sdk "github.com/pinecone-io/go-pinecone/v5/pinecone"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestNewFetchValidation(t *testing.T) {
	deps := depsWith("pc", &Connector{})

	if _, err := newFetch(types.Settings{"ids": "body.ids"}, deps); err == nil {
		t.Error("expected error when connector is missing")
	}
	if _, err := newFetch(types.Settings{"connector": "pc"}, deps); err == nil {
		t.Error("expected error when ids is missing")
	}
	if _, err := newFetch(types.Settings{"connector": "nope", "ids": "body.ids"}, deps); err == nil {
		t.Error("expected error when connector is not configured")
	}
	if _, err := newFetch(types.Settings{"connector": "pc", "ids": "body.("}, deps); err == nil {
		t.Error("expected error compiling a malformed ids expression")
	}
}

func TestNewFetchDefaultResultVar(t *testing.T) {
	proc, err := newFetch(types.Settings{"connector": "pc", "ids": "body.ids"}, depsWith("pc", &Connector{}))
	if err != nil {
		t.Fatalf("newFetch: %v", err)
	}
	p, ok := proc.(*fetchProcessor)
	if !ok {
		t.Fatalf("proc is %T, want *fetchProcessor", proc)
	}
	if p.resultVar != defaultFetchResultVar {
		t.Errorf("resultVar = %q, want default %q", p.resultVar, defaultFetchResultVar)
	}
}

func TestToStringSlice(t *testing.T) {
	ids, err := toStringSlice([]any{"a", "b"})
	if err != nil || len(ids) != 2 || ids[0] != "a" {
		t.Errorf("ids = %+v, err = %v", ids, err)
	}
	if _, err := toStringSlice([]any{1}); err == nil {
		t.Error("expected error for a non-string element")
	}
	if _, err := toStringSlice("not a list"); err == nil {
		t.Error("expected error for a non-list value")
	}
}

func TestVectorFields(t *testing.T) {
	if got := vectorFields(&sdk.Vector{Id: "a"}); len(got) != 0 {
		t.Errorf("bare vector fields = %+v, want empty", got)
	}

	values := []float32{0.1}
	got := vectorFields(&sdk.Vector{Id: "a", Values: &values})
	if _, ok := got["values"]; !ok {
		t.Errorf("expected values field: %+v", got)
	}
	if _, ok := got["metadata"]; ok {
		t.Errorf("expected no metadata field: %+v", got)
	}
}
