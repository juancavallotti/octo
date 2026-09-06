package controlflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// BenchmarkForeachMapMode measures map mode against the size of the collection it
// maps — the shape issue #166 was about.
//
// Read it as a ratio, not an absolute: the element count quadruples at each step,
// so a linear loop quadruples ns/op and B/op with it. A per-element body copy makes
// the same step cost ~16x, because in map mode the body is the collection and every
// one of the n iterations copies all n elements.
//
// The loop body is a single expression, so what is measured is the block's
// per-iteration machinery rather than any work inside it.
func BenchmarkForeachMapMode(b *testing.B) {
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "to-body", Settings: types.Settings{"var": "rec"}},
	}}
	proc := mustBuild(b, mapRegistry(), types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "map", "items": "body.records", "as": "rec", "body": &body}})
	ctx := context.Background()

	for _, n := range []int{100, 400, 1600} {
		records := make([]any, n)
		for i := range records {
			records[i] = map[string]any{"id": fmt.Sprintf("rec-%d", i), "amount": float64(i)}
		}

		b.Run(fmt.Sprintf("records=%d", n), func(b *testing.B) {
			msg := mustMessage(b)
			b.ReportAllocs()
			for b.Loop() {
				// Map mode replaces the body with the collected array, so the body
				// has to be restored or the next pass would map an array of
				// results. It rewraps the same records slice, which is O(1) and
				// independent of n: a constant offset on every size, not a slope.
				msg.Body = map[string]any{"records": records}

				if _, err := proc.Process(ctx, msg); err != nil {
					b.Fatalf("Process: %v", err)
				}
			}
		})
	}
}
