package mongodb

import (
	"encoding/json"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"

	"github.com/juancavallotti/octo/runtime/types"
)

func TestNewInsertValidation(t *testing.T) {
	conn := &Connector{}
	tests := []struct {
		name     string
		settings types.Settings
	}{
		{
			name:     "connector is required",
			settings: types.Settings{"collection": `"orders"`, "document": "body"},
		},
		{
			name:     "collection is required",
			settings: types.Settings{"connector": "orders-db", "document": "body"},
		},
		{
			// Nothing sensible to insert without it, and an empty insert would be
			// a silently useless block.
			name:     "document is required",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`},
		},
		{
			name:     "document must compile",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "document": "body."},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newInsert(tt.settings, depsWith(conn)); err == nil {
				t.Error("expected a build-time error")
			}
		})
	}
}

func TestNewInsertDefaults(t *testing.T) {
	built, err := newInsert(types.Settings{
		"connector":  "orders-db",
		"collection": `"orders"`,
		"document":   "body",
	}, depsWith(&Connector{}))
	if err != nil {
		t.Fatalf("newInsert: %v", err)
	}
	p, ok := built.(*insertProcessor)
	if !ok {
		t.Fatalf("newInsert returned %T, want *insertProcessor", built)
	}
	if p.resultVar != "" {
		t.Errorf("resultVar = %q, want empty (the ids become the body)", p.resultVar)
	}
	// Ordered is MongoDB's default and the safer one: a flow inserting a batch
	// usually wants to know at the first failure, not after the rest landed.
	if p.unordered {
		t.Error("unordered defaults to true, want ordered")
	}
}

// The ids go back as extended JSON so an _id the server generated returns in
// the {"$oid": ...} shape a filter takes. That is what makes "insert it, then
// read it back" work without the flow author converting anything.
func TestInsertResultShape(t *testing.T) {
	oid := bson.NewObjectID()
	p := &insertProcessor{conn: &Connector{}}

	result, err := p.result([]any{oid, "manual-id"})
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if result["insertedCount"] != 2 {
		t.Errorf("insertedCount = %#v, want 2", result["insertedCount"])
	}

	// Rendered the way it will actually reach the message.
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded struct {
		InsertedCount int   `json:"insertedCount"`
		InsertedIDs   []any `json:"insertedIds"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.InsertedCount != 2 {
		t.Errorf("insertedCount = %d, want 2", decoded.InsertedCount)
	}
	generated, ok := decoded.InsertedIDs[0].(map[string]any)
	if !ok {
		t.Fatalf("insertedIds[0] is %T, want the {\"$oid\": ...} object", decoded.InsertedIDs[0])
	}
	if generated["$oid"] != oid.Hex() {
		t.Errorf("insertedIds[0].$oid = %v, want %q", generated["$oid"], oid.Hex())
	}
	// An id the author supplied comes back as what it was, not wrapped.
	if decoded.InsertedIDs[1] != "manual-id" {
		t.Errorf("insertedIds[1] = %#v, want %q", decoded.InsertedIDs[1], "manual-id")
	}
}

// insertedIds is a list even for one document, so a flow reading it does not
// have to branch on how many were written.
func TestInsertResultSingleIsStillAList(t *testing.T) {
	p := &insertProcessor{conn: &Connector{}}
	result, err := p.result([]any{bson.NewObjectID()})
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	ids, ok := result["insertedIds"].([]json.RawMessage)
	if !ok {
		t.Fatalf("insertedIds is %T, want a list", result["insertedIds"])
	}
	if len(ids) != 1 {
		t.Errorf("insertedIds has %d entries, want 1", len(ids))
	}
}

// Nothing inserted is still a well-formed answer: an empty list, not null, so
// the shape does not change on the day the batch filters down to nothing.
func TestInsertResultEmpty(t *testing.T) {
	p := &insertProcessor{conn: &Connector{}}
	result, err := p.result(nil)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	if result["insertedCount"] != 0 {
		t.Errorf("insertedCount = %#v, want 0", result["insertedCount"])
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if got, want := string(raw), `{"insertedCount":0,"insertedIds":[]}`; got != want {
		t.Errorf("result = %s, want %s", got, want)
	}
}
