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
