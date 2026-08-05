package mongodb

import (
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestNewDeleteValidation(t *testing.T) {
	conn := &Connector{}
	tests := []struct {
		name     string
		settings types.Settings
	}{
		{
			name:     "connector is required",
			settings: types.Settings{"collection": `"orders"`, "filter": `{"sku": "A1"}`},
		},
		{
			name:     "collection is required",
			settings: types.Settings{"connector": "orders-db", "filter": `{"sku": "A1"}`},
		},
		{
			// Required even with deleteAll: "match everything" should be
			// something the flow says, not something it omits.
			name:     "filter is required",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`},
		},
		{
			name:     "filter is required even with deleteAll",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "deleteAll": true},
		},
		{
			name:     "filter must compile",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "filter": "body."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newDelete(tt.settings, depsWith(conn)); err == nil {
				t.Error("expected a build-time error")
			}
		})
	}
}

func TestNewDeleteDefaults(t *testing.T) {
	built, err := newDelete(types.Settings{
		"connector":  "orders-db",
		"collection": `"orders"`,
		"filter":     `{"sku": body.sku}`,
	}, depsWith(&Connector{}))
	if err != nil {
		t.Fatalf("newDelete: %v", err)
	}
	p, ok := built.(*deleteProcessor)
	if !ok {
		t.Fatalf("newDelete returned %T, want *deleteProcessor", built)
	}
	// Both default off: removing one document is the smaller blast radius, and
	// emptying a collection should never be reachable without saying so.
	if p.many {
		t.Error("many defaults to true, want one document")
	}
	if p.deleteAll {
		t.Error("deleteAll defaults to true, want off")
	}
}

// The filter is an expression, so an empty one is rarely written on purpose: it
// is what {"customer": body.customerId} collapses to when the field is absent.
// That failure mode empties a collection, which no error afterwards can undo.
func TestGuardEmptyFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    bson.D
		many      bool
		deleteAll bool
		wantErr   bool
	}{
		{
			name:    "empty filter with many is refused",
			filter:  bson.D{},
			many:    true,
			wantErr: true,
		},
		{
			name:      "deleteAll allows it",
			filter:    bson.D{},
			many:      true,
			deleteAll: true,
		},
		{
			// One document is a blast radius of one, and "delete the oldest" is a
			// real thing to ask for.
			name:   "empty filter without many is fine",
			filter: bson.D{},
			many:   false,
		},
		{
			name:   "a real filter with many is fine",
			filter: bson.D{{Key: "sku", Value: "A1"}},
			many:   true,
		},
		{
			// The shape the guard actually exists for: a filter whose only key
			// carries a null the message did not supply still names a key, so it
			// is not the empty-filter case and MongoDB matches on null.
			name:   "a filter naming a key is not empty",
			filter: bson.D{{Key: "customer", Value: nil}},
			many:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := bson.Marshal(tt.filter)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			p := &deleteProcessor{many: tt.many, deleteAll: tt.deleteAll}
			err = p.guardEmptyFilter(bson.Raw(raw))
			if tt.wantErr && err == nil {
				t.Error("expected the guard to refuse this delete")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("guardEmptyFilter: %v", err)
			}
		})
	}
}
