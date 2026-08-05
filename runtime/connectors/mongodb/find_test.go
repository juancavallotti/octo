package mongodb

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func TestNewFindValidation(t *testing.T) {
	conn := &Connector{}
	tests := []struct {
		name     string
		settings types.Settings
		deps     core.BlockDeps
	}{
		{
			name:     "connector is required",
			settings: types.Settings{"collection": `"orders"`},
			deps:     depsWith(conn),
		},
		{
			name:     "connector must exist",
			settings: types.Settings{"connector": "other-db", "collection": `"orders"`},
			deps:     depsWith(conn),
		},
		{
			name:     "collection is required",
			settings: types.Settings{"connector": "orders-db"},
			deps:     depsWith(conn),
		},
		{
			name:     "filter must compile",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "filter": "body."},
			deps:     depsWith(conn),
		},
		{
			name:     "projection must compile",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "projection": "body."},
			deps:     depsWith(conn),
		},
		{
			name:     "sort must compile",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "sort": "body."},
			deps:     depsWith(conn),
		},
		{
			// A negative limit is meaningless to the driver, so catching it at
			// build time turns a per-message surprise into a startup error.
			name:     "limit must not be negative",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "limit": -1},
			deps:     depsWith(conn),
		},
		{
			name:     "skip must not be negative",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "skip": -1},
			deps:     depsWith(conn),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newFind(tt.settings, tt.deps); err == nil {
				t.Error("expected a build-time error")
			}
		})
	}
}

func TestNewFindDefaults(t *testing.T) {
	built, err := newFind(types.Settings{
		"connector":  "orders-db",
		"collection": `"orders"`,
	}, depsWith(&Connector{}))
	if err != nil {
		t.Fatalf("newFind: %v", err)
	}
	p, ok := built.(*findProcessor)
	if !ok {
		t.Fatalf("newFind returned %T, want *findProcessor", built)
	}

	// Empty is the default, and empty means "the documents are the body" — see
	// deliver. A default variable name here would make the block silently keep a
	// body nobody asked it to keep.
	if p.resultVar != "" {
		t.Errorf("resultVar = %q, want empty (the documents become the body)", p.resultVar)
	}
	// No filter is not an empty filter the block has to build: an absent
	// expression stays absent until the driver needs one.
	if p.filter != nil {
		t.Error("filter program built for an unset filter")
	}
	if p.single {
		t.Error("single defaults to true, want a list")
	}
	if p.limit != 0 || p.skip != 0 {
		t.Errorf("limit/skip = %d/%d, want 0/0", p.limit, p.skip)
	}
}

func TestOrEmptyFilter(t *testing.T) {
	doc, err := bson.Marshal(bson.D{{Key: "sku", Value: "A1"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got := orEmpty(bson.Raw(doc)); got == nil {
		t.Error("orEmpty dropped a real filter")
	}
	// The driver wants a filter either way, and "no filter" and "match
	// everything" are the same query.
	if got := orEmpty(nil); got == nil {
		t.Error("orEmpty(nil) = nil, want the empty document")
	}
}

func TestDeliverMiss(t *testing.T) {
	t.Run("body", func(t *testing.T) {
		msg := newMessage(t, map[string]any{"sku": "A1"})
		deliverMiss(msg, "")
		if msg.Body != nil {
			t.Errorf("body = %#v, want nil", msg.Body)
		}
	})

	t.Run("variable", func(t *testing.T) {
		msg := newMessage(t, map[string]any{"sku": "A1"})
		deliverMiss(msg, "order")
		if msg.Body == nil {
			t.Error("body was cleared even though the block named a variable")
		}
		value, ok := msg.Variables["order"]
		if !ok {
			t.Fatal("vars.order was not set; a miss has to be distinguishable from an unrun block")
		}
		if value != nil {
			t.Errorf("vars.order = %#v, want nil", value)
		}
	})
}
