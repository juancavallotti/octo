package pinecone

import (
	"testing"

	sdk "github.com/pinecone-io/go-pinecone/v5/pinecone"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestNewQueryValidation(t *testing.T) {
	deps := depsWith("pc", &Connector{})

	if _, err := newQuery(types.Settings{"vector": "body.vector"}, deps); err == nil {
		t.Error("expected error when connector is missing")
	}
	if _, err := newQuery(types.Settings{"connector": "pc"}, deps); err == nil {
		t.Error("expected error when vector is missing")
	}
	if _, err := newQuery(types.Settings{"connector": "nope", "vector": "body.vector"}, deps); err == nil {
		t.Error("expected error when connector is not configured")
	}
	if _, err := newQuery(types.Settings{"connector": "pc", "vector": "body.vector", "filter": "body.("}, deps); err == nil {
		t.Error("expected error compiling a malformed filter expression")
	}
}

func TestNewQueryDefaults(t *testing.T) {
	proc, err := newQuery(types.Settings{"connector": "pc", "vector": "body.vector"}, depsWith("pc", &Connector{}))
	if err != nil {
		t.Fatalf("newQuery: %v", err)
	}
	p, ok := proc.(*queryProcessor)
	if !ok {
		t.Fatalf("proc is %T, want *queryProcessor", proc)
	}
	if p.topK != defaultTopK {
		t.Errorf("topK = %d, want default %d", p.topK, defaultTopK)
	}
	if !p.includeMetadata {
		t.Error("includeMetadata should default to true")
	}
	if p.resultVar != defaultQueryResultVar {
		t.Errorf("resultVar = %q, want default %q", p.resultVar, defaultQueryResultVar)
	}
}

func TestToMatch(t *testing.T) {
	values := []float32{0.1, 0.2}
	meta, err := sdk.NewMetadata(map[string]any{"category": "docs"})
	if err != nil {
		t.Fatalf("NewMetadata: %v", err)
	}

	match := toMatch(&sdk.ScoredVector{
		Score:  0.9,
		Vector: &sdk.Vector{Id: "a", Values: &values, Metadata: meta},
	})
	if match["id"] != "a" || match["score"] != float32(0.9) {
		t.Errorf("match = %+v", match)
	}
	gotValues, ok := match["values"].([]float32)
	if !ok || len(gotValues) != 2 {
		t.Errorf("match values = %+v", match["values"])
	}
	gotMeta, ok := match["metadata"].(map[string]any)
	if !ok || gotMeta["category"] != "docs" {
		t.Errorf("match metadata = %+v", match["metadata"])
	}

	// A match with no vector (values/metadata withheld by the query settings)
	// still reports its score without panicking.
	bare := toMatch(&sdk.ScoredVector{Score: 0.5})
	if bare["score"] != float32(0.5) || bare["id"] != nil {
		t.Errorf("bare match = %+v", bare)
	}
}
