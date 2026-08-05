package mongodb

import (
	"encoding/json"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func TestNewUpdateValidation(t *testing.T) {
	conn := &Connector{}
	base := func(extra types.Settings) types.Settings {
		settings := types.Settings{
			"connector":  "orders-db",
			"collection": `"orders"`,
			"filter":     `{"sku": body.sku}`,
		}
		for k, v := range extra {
			settings[k] = v
		}
		return settings
	}

	tests := []struct {
		name     string
		settings types.Settings
	}{
		{
			// Without one, the block would match documents and do nothing to them.
			name:     "neither update nor replacement",
			settings: base(nil),
		},
		{
			// They are different operations wearing similar clothes: an update
			// applies operators to what is there, a replacement discards it.
			name: "both update and replacement",
			settings: base(types.Settings{
				"update":      `{"$set": {"qty": 1}}`,
				"replacement": "body",
			}),
		},
		{
			// There is no ReplaceMany, for the same reason nobody means it.
			name: "replacement with many",
			settings: base(types.Settings{
				"replacement": "body",
				"many":        true,
			}),
		},
		{
			name:     "filter is required",
			settings: types.Settings{"connector": "orders-db", "collection": `"orders"`, "update": `{"$set": {}}`},
		},
		{
			name:     "filter must compile",
			settings: base(types.Settings{"filter": "body.", "update": `{"$set": {}}`}),
		},
		{
			name:     "update must compile",
			settings: base(types.Settings{"update": "body."}),
		},
		{
			name:     "replacement must compile",
			settings: base(types.Settings{"replacement": "body."}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newUpdate(tt.settings, depsWith(conn)); err == nil {
				t.Error("expected a build-time error")
			}
		})
	}
}

func TestNewUpdatePicksTheMode(t *testing.T) {
	tests := []struct {
		name        string
		settings    types.Settings
		wantReplace bool
	}{
		{
			name:        "update",
			settings:    types.Settings{"update": `{"$set": {"qty": 1}}`},
			wantReplace: false,
		},
		{
			name:        "replacement",
			settings:    types.Settings{"replacement": "body"},
			wantReplace: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := types.Settings{
				"connector":  "orders-db",
				"collection": `"orders"`,
				"filter":     `{"sku": "A1"}`,
			}
			for k, v := range tt.settings {
				settings[k] = v
			}
			built, err := newUpdate(settings, depsWith(&Connector{}))
			if err != nil {
				t.Fatalf("newUpdate: %v", err)
			}
			p, ok := built.(*updateProcessor)
			if !ok {
				t.Fatalf("newUpdate returned %T, want *updateProcessor", built)
			}
			if p.replace != tt.wantReplace {
				t.Errorf("replace = %v, want %v", p.replace, tt.wantReplace)
			}
			if p.upsert {
				t.Error("upsert defaults to true; it should not create documents unasked")
			}
		})
	}
}

// A plain object passed as an update is refused by MongoDB with a message about
// the first field name, which says nothing about the mistake made. Catching it
// here means the error names the fix.
func TestRequireOperators(t *testing.T) {
	tests := []struct {
		name    string
		doc     bson.D
		wantErr bool
	}{
		{name: "set", doc: bson.D{{Key: "$set", Value: bson.D{{Key: "qty", Value: 1}}}}},
		{
			name: "several operators",
			doc: bson.D{
				{Key: "$set", Value: bson.D{{Key: "qty", Value: 1}}},
				{Key: "$inc", Value: bson.D{{Key: "version", Value: 1}}},
			},
		},
		{name: "plain fields", doc: bson.D{{Key: "qty", Value: 1}}, wantErr: true},
		{
			name: "one plain field among operators",
			doc: bson.D{
				{Key: "$set", Value: bson.D{{Key: "qty", Value: 1}}},
				{Key: "status", Value: "open"},
			},
			wantErr: true,
		},
		{name: "empty", doc: bson.D{}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := bson.Marshal(tt.doc)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			err = requireOperators(bson.Raw(raw))
			if tt.wantErr && err == nil {
				t.Error("expected an error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("requireOperators: %v", err)
			}
		})
	}
}

// An aggregation pipeline is a valid update, and its stages are operators
// already, so the operator check does not apply to it.
func TestEvalChangeAcceptsAPipeline(t *testing.T) {
	program, err := expr.CompileMessage(nil, `[{"$set": {"total": {"$multiply": ["$qty", "$price"]}}}]`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	p := &updateProcessor{change: program}
	msg := newMessage(t, nil)

	change, err := p.evalChange(expr.MessageActivation(msg, nil))
	if err != nil {
		t.Fatalf("evalChange: %v", err)
	}
	stages, ok := change.([]any)
	if !ok {
		t.Fatalf("change is %T, want a list of stages", change)
	}
	if len(stages) != 1 {
		t.Errorf("stages = %d, want 1", len(stages))
	}
}

func TestEvalChangeRejectsAPlainObject(t *testing.T) {
	program, err := expr.CompileMessage(nil, `{"qty": 1}`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	p := &updateProcessor{change: program}
	msg := newMessage(t, nil)

	if _, err := p.evalChange(expr.MessageActivation(msg, nil)); err == nil {
		t.Error("expected an error naming $set for an update with no operators")
	}
}

// A replacement is a whole document, so it takes plain fields — the check that
// applies to an update must not apply to it.
func TestEvalChangeReplacementTakesPlainFields(t *testing.T) {
	program, err := expr.CompileMessage(nil, `{"sku": "A1", "qty": 3}`)
	if err != nil {
		t.Fatalf("CompileMessage: %v", err)
	}
	p := &updateProcessor{change: program, replace: true}
	msg := newMessage(t, nil)

	if _, err := p.evalChange(expr.MessageActivation(msg, nil)); err != nil {
		t.Errorf("evalChange: %v", err)
	}
}

// matchedCount and modifiedCount differ when an update sets a field to the value
// it already held — "nothing matched" and "nothing changed" are different bugs,
// so both are reported.
func TestUpdateResultShape(t *testing.T) {
	p := &updateProcessor{conn: &Connector{}}
	rendered, err := p.result(&mongo.UpdateResult{MatchedCount: 3, ModifiedCount: 1})
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	raw, err := json.Marshal(rendered)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"matchedCount":3,"modifiedCount":1,"upsertedCount":0,"upsertedId":null}`
	if got := string(raw); got != want {
		t.Errorf("result = %s, want %s", got, want)
	}
}

// upsertedId is present and null when no upsert happened, rather than absent, so
// a flow reading it does not have to test for the field before its value.
func TestUpdateResultUpsertedID(t *testing.T) {
	oid := bson.NewObjectID()
	p := &updateProcessor{conn: &Connector{}}
	rendered, err := p.result(&mongo.UpdateResult{UpsertedCount: 1, UpsertedID: oid})
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	raw, err := json.Marshal(rendered)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded struct {
		UpsertedID map[string]any `json:"upsertedId"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	// The same {"$oid": ...} shape a filter takes, so the upserted document can
	// be read back without converting anything.
	if decoded.UpsertedID["$oid"] != oid.Hex() {
		t.Errorf("upsertedId.$oid = %v, want %q", decoded.UpsertedID["$oid"], oid.Hex())
	}
}
