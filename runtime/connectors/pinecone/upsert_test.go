package pinecone

import (
	"fmt"
	"testing"

	sdk "github.com/pinecone-io/go-pinecone/v5/pinecone"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// depsWith wires a connector under the name "pc", which every test uses.
func depsWith(_ string, conn core.Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(n string) (core.Connector, bool) {
		if n == "pc" {
			return conn, true
		}
		return nil, false
	}}
}

func TestNewUpsertValidation(t *testing.T) {
	deps := depsWith("pc", &Connector{})

	if _, err := newUpsert(types.Settings{"vectors": "body.vectors"}, deps); err == nil {
		t.Error("expected error when connector is missing")
	}
	if _, err := newUpsert(types.Settings{"connector": "pc"}, deps); err == nil {
		t.Error("expected error when vectors is missing")
	}
	if _, err := newUpsert(types.Settings{"connector": "nope", "vectors": "body.vectors"}, deps); err == nil {
		t.Error("expected error when connector is not configured")
	}
	if _, err := newUpsert(types.Settings{"connector": "pc", "vectors": "body.("}, deps); err == nil {
		t.Error("expected error compiling a malformed vectors expression")
	}
}

func TestChunkVectors(t *testing.T) {
	mk := func(n int) []*sdk.Vector {
		out := make([]*sdk.Vector, n)
		for i := range out {
			out[i] = &sdk.Vector{Id: fmt.Sprintf("v%d", i)}
		}
		return out
	}

	cases := []struct {
		name       string
		n, size    int
		wantChunks []int
	}{
		{"empty", 0, 100, nil},
		{"under one chunk", 5, 100, []int{5}},
		{"exactly one chunk", 100, 100, []int{100}},
		{"two chunks", 150, 100, []int{100, 50}},
		{"exactly two chunks", 200, 100, []int{100, 100}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			chunks := chunkVectors(mk(tt.n), tt.size)
			if len(chunks) != len(tt.wantChunks) {
				t.Fatalf("chunk count = %d, want %d", len(chunks), len(tt.wantChunks))
			}
			for i, want := range tt.wantChunks {
				if len(chunks[i]) != want {
					t.Errorf("chunk %d size = %d, want %d", i, len(chunks[i]), want)
				}
			}
		})
	}
}

func TestToVector(t *testing.T) {
	vec, err := toVector(map[string]any{
		"id":       "a",
		"values":   []any{0.1, 0.2},
		"metadata": map[string]any{"category": "docs"},
	})
	if err != nil {
		t.Fatalf("toVector: %v", err)
	}
	if vec.Id != "a" || vec.Values == nil || len(*vec.Values) != 2 || vec.Metadata == nil {
		t.Errorf("vec = %+v", vec)
	}

	if _, err := toVector(map[string]any{"values": []any{0.1}}); err == nil {
		t.Error("expected error for missing id")
	}
	if _, err := toVector(map[string]any{"id": "a"}); err == nil {
		t.Error("expected error for missing values")
	}
	if _, err := toVector(map[string]any{"id": "a", "values": []any{"x"}}); err == nil {
		t.Error("expected error for non-numeric value")
	}
	if _, err := toVector("not an object"); err == nil {
		t.Error("expected error for a non-object entry")
	}
}
