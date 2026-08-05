package mongodb

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func TestNewAggregateValidation(t *testing.T) {
	conn := &Connector{}
	tests := []struct {
		name     string
		settings types.Settings
	}{
		{
			name:     "connector is required",
			settings: types.Settings{"collection": `"orders"`, "pipeline": `[{"$count": "n"}]`},
		},
		{
			name:     "collection is required",
			settings: types.Settings{"connector": "orders-db", "pipeline": `[{"$count": "n"}]`},
		},
		{
			name:     "pipeline is required",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`},
		},
		{
			name:     "pipeline must compile",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "pipeline": "body."},
		},
		{
			name: "limit must not be negative",
			settings: types.Settings{
				"connector": "orders-db", "collection": `"orders"`,
				"pipeline": `[{"$count": "n"}]`, "limit": -1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newAggregate(tt.settings, depsWith(conn)); err == nil {
				t.Error("expected a build-time error")
			}
		})
	}
}

func TestNewAggregateDefaults(t *testing.T) {
	built, err := newAggregate(types.Settings{
		"connector":  "orders-db",
		"collection": `"orders"`,
		"pipeline":   `[{"$count": "n"}]`,
	}, depsWith(&Connector{}))
	if err != nil {
		t.Fatalf("newAggregate: %v", err)
	}
	p, ok := built.(*aggregateProcessor)
	if !ok {
		t.Fatalf("newAggregate returned %T, want *aggregateProcessor", built)
	}
	if p.resultVar != "" {
		t.Errorf("resultVar = %q, want empty (the documents become the body)", p.resultVar)
	}
	if p.limit != 0 {
		t.Errorf("limit = %d, want 0 (a pipeline says what it returns)", p.limit)
	}
}

// A limit bounds the pipeline server-side, so the cursor never reads what the
// block would have thrown away.
func TestAggregateBound(t *testing.T) {
	stages, err := decodePipeline("pipeline", []any{
		map[string]any{"$match": map[string]any{"sku": "A1"}},
	})
	if err != nil {
		t.Fatalf("decodePipeline: %v", err)
	}

	t.Run("unset leaves the pipeline alone", func(t *testing.T) {
		p := &aggregateProcessor{}
		got, err := p.bound(stages)
		if err != nil {
			t.Fatalf("bound: %v", err)
		}
		if len(got) != len(stages) {
			t.Errorf("stages = %d, want the %d written", len(got), len(stages))
		}
	})

	t.Run("appends a final $limit", func(t *testing.T) {
		p := &aggregateProcessor{limit: 50}
		got, err := p.bound(stages)
		if err != nil {
			t.Fatalf("bound: %v", err)
		}
		if len(got) != len(stages)+1 {
			t.Fatalf("stages = %d, want %d", len(got), len(stages)+1)
		}
		value, err := got[len(got)-1].LookupErr(limitStage)
		if err != nil {
			t.Fatalf("the appended stage is not a %s: %v", limitStage, err)
		}
		if n, ok := value.AsInt64OK(); !ok || n != 50 {
			t.Errorf("%s = %v, want 50", limitStage, value)
		}
	})
}

// $out and $merge must themselves be last, so appending to one would produce a
// server error about stage order rather than about the setting that caused it.
func TestAggregateBoundRefusesAWritingPipeline(t *testing.T) {
	for _, stage := range []string{"$out", "$merge"} {
		t.Run(stage, func(t *testing.T) {
			stages, err := decodePipeline("pipeline", []any{
				map[string]any{"$match": map[string]any{"sku": "A1"}},
				map[string]any{stage: "totals"},
			})
			if err != nil {
				t.Fatalf("decodePipeline: %v", err)
			}
			p := &aggregateProcessor{limit: 50}
			if _, err := p.bound(stages); err == nil {
				t.Errorf("expected an error for a limit over a pipeline ending in %s", stage)
			}
		})
	}
}

// The stages an author would type into mongosh are the stages they write here,
// with body and vars reachable inside them — that is what makes a pipeline
// worth expressing in a definition rather than hiding behind settings.
func TestAggregatePipelineReadsTheMessage(t *testing.T) {
	program, err := expr.CompileMessage(nil,
		`[{"$match": {"customer": body.customerId}}, {"$group": {"_id": "$sku", "n": {"$sum": 1}}}]`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	msg := newMessage(t, map[string]any{"customerId": "acme"})

	value, err := program.Eval(expr.MessageActivation(msg, nil))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	stages, err := decodePipeline("pipeline", value)
	if err != nil {
		t.Fatalf("decodePipeline: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(stages))
	}
	if got := stages[0].Lookup("$match", "customer").StringValue(); got != "acme" {
		t.Errorf("$match.customer = %q, want %q from the body", got, "acme")
	}
}

// A pipeline written as a bare stage is a mistake worth naming against the
// field, rather than a driver error further down.
func TestAggregateRejectsABareStage(t *testing.T) {
	program, err := expr.CompileMessage(nil, `{"$count": "n"}`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	msg := newMessage(t, nil)
	value, err := program.Eval(expr.MessageActivation(msg, nil))
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if _, err := decodePipeline("pipeline", value); err == nil {
		t.Error("expected an error for a pipeline that is not a list of stages")
	}
}
