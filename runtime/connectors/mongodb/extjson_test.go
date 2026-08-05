package mongodb

import (
	"encoding/json"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

// The claim this whole file exists to defend: a document that comes out of a
// find can be handed straight back to a filter and still name the same types.
// A flattening convention (ObjectId to a hex string, dates to RFC 3339) fails
// exactly here, and fails silently.
func TestRoundTripPreservesBSONTypes(t *testing.T) {
	oid := bson.NewObjectID()
	decimal, err := bson.ParseDecimal128("1234.56")
	if err != nil {
		t.Fatalf("ParseDecimal128: %v", err)
	}
	original := bson.D{
		{Key: "_id", Value: oid},
		{Key: "created", Value: bson.NewDateTimeFromTime(time.Date(2026, 8, 4, 10, 0, 0, 0, time.UTC))},
		{Key: "price", Value: decimal},
		{Key: "sequence", Value: int64(9007199254740993)},
		{Key: "qty", Value: int32(2)},
		{Key: "blob", Value: bson.Binary{Subtype: 0x00, Data: []byte{1, 2, 3}}},
		{Key: "sku", Value: "A1"},
		{Key: "nested", Value: bson.D{{Key: "ref", Value: oid}}},
	}

	for _, canonical := range []bool{false, true} {
		name := "relaxed"
		if canonical {
			name = "canonical"
		}
		t.Run(name, func(t *testing.T) {
			doc, err := bson.Marshal(original)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			// Out to the message, exactly as a block would render it...
			out, err := encodeDocument(doc, canonical)
			if err != nil {
				t.Fatalf("encodeDocument: %v", err)
			}
			// ...and back in as a filter would arrive, having passed through a
			// message body: decoded to JSON-native Go values, then re-encoded.
			var asMessageValue any
			if err := json.Unmarshal(out, &asMessageValue); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			back, err := decodeDocument("filter", asMessageValue)
			if err != nil {
				t.Fatalf("decodeDocument: %v", err)
			}

			assertSameType(t, back, "_id", bson.TypeObjectID)
			assertSameType(t, back, "created", bson.TypeDateTime)
			assertSameType(t, back, "price", bson.TypeDecimal128)
			assertSameType(t, back, "blob", bson.TypeBinary)
			assertSameType(t, back, "sku", bson.TypeString)

			if got := back.Lookup("_id").ObjectID(); got != oid {
				t.Errorf("_id = %v, want %v", got, oid)
			}
			if got := back.Lookup("nested", "ref").ObjectID(); got != oid {
				t.Errorf("nested.ref = %v, want %v", got, oid)
			}

			// The one type relaxed does not carry, pinned here so the caveat the
			// reference page documents is a tested fact: an integer past 2^53
			// goes out as a bare JSON number and comes back through the
			// message's float64 numbers, losing its last digit. Canonical types
			// it and it survives exactly.
			if canonical {
				assertSameType(t, back, "sequence", bson.TypeInt64)
				if got := back.Lookup("sequence").Int64(); got != 9007199254740993 {
					t.Errorf("sequence = %d, want 9007199254740993", got)
				}
			} else if got, _ := back.Lookup("sequence").AsInt64OK(); got == 9007199254740993 {
				t.Error("relaxed mode preserved an int64 past 2^53; the documented caveat is stale")
			}
		})
	}
}

// Relaxed renders ordinary numbers as plain JSON, which is the whole reason it
// is the default -- and the whole reason it is not the only mode: an int64
// past 2^53 does not survive the message's float64 numbers.
func TestRelaxedIsReadableCanonicalIsExact(t *testing.T) {
	doc, err := bson.Marshal(bson.D{
		{Key: "qty", Value: int32(2)},
		{Key: "sequence", Value: int64(9007199254740993)},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	relaxed, err := encodeDocument(doc, false)
	if err != nil {
		t.Fatalf("encodeDocument: %v", err)
	}
	if got, want := string(relaxed), `{"qty":2,"sequence":9007199254740993}`; got != want {
		t.Errorf("relaxed = %s, want %s", got, want)
	}

	canonical, err := encodeDocument(doc, true)
	if err != nil {
		t.Fatalf("encodeDocument: %v", err)
	}
	want := `{"qty":{"$numberInt":"2"},"sequence":{"$numberLong":"9007199254740993"}}`
	if got := string(canonical); got != want {
		t.Errorf("canonical = %s, want %s", got, want)
	}
}

// Both spellings parse no matter which mode renders results, so a filter is
// never rejected for being written in the "wrong" one.
func TestDecodeAcceptsBothSpellings(t *testing.T) {
	oid := bson.NewObjectID()
	tests := []struct {
		name  string
		value any
	}{
		{name: "relaxed", value: map[string]any{"seq": float64(3)}},
		{name: "canonical", value: map[string]any{"seq": map[string]any{"$numberLong": "3"}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := decodeDocument("filter", tt.value)
			if err != nil {
				t.Fatalf("decodeDocument: %v", err)
			}
			if got, ok := doc.Lookup("seq").AsInt64OK(); !ok || got != 3 {
				t.Errorf("seq = %v, want 3", doc.Lookup("seq"))
			}
		})
	}

	// The form an author writes to match an _id they read out of a body.
	doc, err := decodeDocument("filter", map[string]any{"_id": map[string]any{"$oid": oid.Hex()}})
	if err != nil {
		t.Fatalf("decodeDocument: %v", err)
	}
	if got := doc.Lookup("_id").ObjectID(); got != oid {
		t.Errorf("_id = %v, want %v", got, oid)
	}
}

func TestDecodeDocumentRejectsNonObjects(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "list", value: []any{map[string]any{"a": 1}}},
		{name: "string", value: "sku"},
		{name: "number", value: float64(1)},
		{name: "null", value: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeDocument("filter", tt.value); err == nil {
				t.Errorf("expected an error for %T", tt.value)
			}
		})
	}
}

func TestDecodePipeline(t *testing.T) {
	stages, err := decodePipeline("pipeline", []any{
		map[string]any{"$match": map[string]any{"sku": "A1"}},
		map[string]any{"$count": "total"},
	})
	if err != nil {
		t.Fatalf("decodePipeline: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("stages = %d, want 2", len(stages))
	}
	if got := stages[1].Lookup("$count").StringValue(); got != "total" {
		t.Errorf("stage[1].$count = %q, want %q", got, "total")
	}
}

// A one-stage pipeline written without its brackets is a mistake worth naming
// against the field, rather than a driver error further down.
func TestDecodePipelineRejectsBareDocument(t *testing.T) {
	if _, err := decodePipeline("pipeline", map[string]any{"$match": map[string]any{}}); err == nil {
		t.Error("expected an error for a pipeline that is not a list")
	}
}

// A find that matched nothing is an empty list, so a flow iterating the body
// gets zero iterations rather than a type error on null.
func TestEncodeDocumentsEmptyIsEmptyArray(t *testing.T) {
	raw, err := encodeDocuments(nil, false)
	if err != nil {
		t.Fatalf("encodeDocuments: %v", err)
	}
	if got := string(raw); got != "[]" {
		t.Errorf("encodeDocuments(nil) = %s, want []", got)
	}
}

func TestEncodeDocuments(t *testing.T) {
	first, err := bson.Marshal(bson.D{{Key: "sku", Value: "A1"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	second, err := bson.Marshal(bson.D{{Key: "sku", Value: "B2"}})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	raw, err := encodeDocuments([]bson.Raw{first, second}, false)
	if err != nil {
		t.Fatalf("encodeDocuments: %v", err)
	}
	if got, want := string(raw), `[{"sku":"A1"},{"sku":"B2"}]`; got != want {
		t.Errorf("encodeDocuments = %s, want %s", got, want)
	}
}

// An inserted _id goes back to the flow as the {"$oid": ...} shape a filter
// takes, which is what makes "insert then read it back" work without the author
// converting anything.
func TestEncodeValue(t *testing.T) {
	oid := bson.NewObjectID()
	raw, err := encodeValue(oid, false)
	if err != nil {
		t.Fatalf("encodeValue: %v", err)
	}
	want := `{"$oid":"` + oid.Hex() + `"}`
	if got := string(raw); got != want {
		t.Errorf("encodeValue = %s, want %s", got, want)
	}
}

// Sorting by the wrong key first is a wrong answer that looks like a right one,
// so a multi-key object — which cannot carry order through CEL or JSON — is
// refused rather than silently reordered.
func TestDecodeSort(t *testing.T) {
	tests := []struct {
		name    string
		value   any
		want    bson.D
		wantErr bool
	}{
		{
			name:  "single key object",
			value: map[string]any{"created": float64(-1)},
			want:  bson.D{{Key: "created", Value: int32(-1)}},
		},
		{
			name: "list keeps the order written",
			value: []any{
				map[string]any{"created": float64(-1)},
				map[string]any{"sku": float64(1)},
			},
			want: bson.D{
				{Key: "created", Value: int32(-1)},
				{Key: "sku", Value: int32(1)},
			},
		},
		{
			name:  "empty object is no sort",
			value: map[string]any{},
			want:  nil,
		},
		{
			name:    "multi-key object is refused",
			value:   map[string]any{"created": float64(-1), "sku": float64(1)},
			wantErr: true,
		},
		{
			name:    "list element must name exactly one key",
			value:   []any{map[string]any{"created": float64(-1), "sku": float64(1)}},
			wantErr: true,
		},
		{
			name:    "list element must be an object",
			value:   []any{"created"},
			wantErr: true,
		},
		{
			name:    "scalar is refused",
			value:   "created",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeSort(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %#v", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("decodeSort: %v", err)
			}
			assertSortEqual(t, got, tt.want)
		})
	}
}

func assertSortEqual(t *testing.T, got, want bson.D) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("sort = %v, want %v", got, want)
	}
	for i := range want {
		if got[i].Key != want[i].Key {
			t.Errorf("sort[%d].Key = %q, want %q", i, got[i].Key, want[i].Key)
		}
		value, ok := got[i].Value.(bson.RawValue)
		if !ok {
			t.Fatalf("sort[%d].Value is %T, want bson.RawValue", i, got[i].Value)
		}
		if got, want := value.Int32(), want[i].Value.(int32); got != want {
			t.Errorf("sort[%d].Value = %d, want %d", i, got, want)
		}
	}
}

// A $sort stage inside a pipeline has the same key-order problem the find
// block's sort setting has, and no guard of its own until now: the keys arrive
// in an unordered CEL map and the JSON on the way to BSON sorts them
// alphabetically. So the pipeline decoder rebuilds $sort, and refuses the form
// that cannot carry order.
func TestDecodeStageRebuildsSort(t *testing.T) {
	stages, err := decodePipeline("pipeline", []any{
		map[string]any{"$match": map[string]any{"sku": "A1"}},
		map[string]any{"$sort": []any{
			map[string]any{"total": float64(-1)},
			map[string]any{"sku": float64(1)},
		}},
	})
	if err != nil {
		t.Fatalf("decodePipeline: %v", err)
	}

	sort, err := stages[1].LookupErr("$sort")
	if err != nil {
		t.Fatalf("lookup $sort: %v", err)
	}
	doc, ok := sort.DocumentOK()
	if !ok {
		t.Fatalf("$sort is %s, want a document", sort.Type)
	}
	elements, err := doc.Elements()
	if err != nil {
		t.Fatalf("Elements: %v", err)
	}
	if len(elements) != 2 {
		t.Fatalf("$sort has %d keys, want 2", len(elements))
	}
	// The order written, not the alphabetical order sku/total would land in.
	if got := elements[0].Key(); got != "total" {
		t.Errorf("$sort primary key = %q, want %q", got, "total")
	}
	if got := elements[1].Key(); got != "sku" {
		t.Errorf("$sort secondary key = %q, want %q", got, "sku")
	}
}

func TestDecodeStageRefusesUnorderableSort(t *testing.T) {
	_, err := decodePipeline("pipeline", []any{
		map[string]any{"$sort": map[string]any{"total": float64(-1), "sku": float64(1)}},
	})
	if err == nil {
		t.Error("expected a multi-key $sort object to be refused")
	}
}

// A single-key $sort has no order to lose, so it passes through as written.
func TestDecodeStageSingleKeySortIsFine(t *testing.T) {
	stages, err := decodePipeline("pipeline", []any{
		map[string]any{"$sort": map[string]any{"total": float64(-1)}},
	})
	if err != nil {
		t.Fatalf("decodePipeline: %v", err)
	}
	if got := stages[0].Lookup("$sort", "total").Int32(); got != -1 {
		t.Errorf("$sort.total = %d, want -1", got)
	}
}

// A stage that merely mentions $sort alongside other keys is not the $sort
// stage, so it takes the ordinary decode.
func TestDecodeStageOnlyRewritesTheSortStage(t *testing.T) {
	stages, err := decodePipeline("pipeline", []any{
		map[string]any{"$match": map[string]any{"$sort": "a field literally called $sort"}},
	})
	if err != nil {
		t.Fatalf("decodePipeline: %v", err)
	}
	if got := stages[0].Lookup("$match", "$sort").StringValue(); got != "a field literally called $sort" {
		t.Errorf("$match.$sort = %q, want it left alone", got)
	}
}

func assertSameType(t *testing.T, doc bson.Raw, key string, want bson.Type) {
	t.Helper()
	value, err := doc.LookupErr(key)
	if err != nil {
		t.Fatalf("lookup %q: %v", key, err)
	}
	if value.Type != want {
		t.Errorf("%s is %s, want %s", key, value.Type, want)
	}
}
