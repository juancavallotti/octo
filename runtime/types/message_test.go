package types

import (
	"encoding/json"
	"reflect"
	"testing"
)

const expectedEventIDLen = eventIDBytes * 2 // hex doubles the byte count

func TestNewMessage(t *testing.T) {
	const correlationID = "corr-123"

	msg, err := NewMessage(correlationID)
	if err != nil {
		t.Fatalf("NewMessage returned error: %v", err)
	}

	if got := len(msg.EventID); got != expectedEventIDLen {
		t.Errorf("EventID length = %d, want %d", got, expectedEventIDLen)
	}
	if msg.CorrelationID != correlationID {
		t.Errorf("CorrelationID = %q, want %q", msg.CorrelationID, correlationID)
	}
	if msg.Variables == nil {
		t.Error("Variables is nil, want initialized map")
	}

	other, err := NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage returned error: %v", err)
	}
	if msg.EventID == other.EventID {
		t.Errorf("EventIDs are not unique: %q", msg.EventID)
	}
}

func TestMessageBodyRoundTrip(t *testing.T) {
	type payload struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	tests := []struct {
		name string
		raw  string
		want any
	}{
		{name: "object", raw: `{"name":"a","count":2}`, want: payload{Name: "a", Count: 2}},
		{name: "array", raw: `[1,2,3]`, want: []any{float64(1), float64(2), float64(3)}},
		{name: "scalar", raw: `"hello"`, want: "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{}
			if err := msg.SetBodyJSON([]byte(tt.raw)); err != nil {
				t.Fatalf("SetBodyJSON returned error: %v", err)
			}

			got, err := msg.BodyJSON()
			if err != nil {
				t.Fatalf("BodyJSON returned error: %v", err)
			}
			if !json.Valid(got) {
				t.Fatalf("BodyJSON returned invalid JSON: %s", got)
			}

			switch want := tt.want.(type) {
			case payload:
				var decoded payload
				if err := msg.DecodeBody(&decoded); err != nil {
					t.Fatalf("DecodeBody returned error: %v", err)
				}
				if decoded != want {
					t.Errorf("DecodeBody = %+v, want %+v", decoded, want)
				}
			default:
				if !reflect.DeepEqual(msg.Body, tt.want) {
					t.Errorf("Body = %#v, want %#v", msg.Body, tt.want)
				}
			}
		})
	}
}

func TestDecodeBodyEmpty(t *testing.T) {
	msg := &Message{}
	var target map[string]any
	if err := msg.DecodeBody(&target); err == nil {
		t.Error("DecodeBody on empty body = nil error, want error")
	}
}

func TestMessageJSONRoundTrip(t *testing.T) {
	msg, err := NewMessage("corr-1")
	if err != nil {
		t.Fatalf("NewMessage returned error: %v", err)
	}
	msg.Variables.Set("flag", true)
	if err := msg.SetBodyJSON([]byte(`{"k":"v"}`)); err != nil {
		t.Fatalf("SetBodyJSON returned error: %v", err)
	}
	msg.BodySchema = json.RawMessage(`{"type":"object"}`)

	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}

	for _, tag := range []string{"event_id", "correlation_id", "variables", "body", "body_schema"} {
		var asMap map[string]json.RawMessage
		if err := json.Unmarshal(raw, &asMap); err != nil {
			t.Fatalf("Unmarshal to map returned error: %v", err)
		}
		if _, ok := asMap[tag]; !ok {
			t.Errorf("marshaled message missing tag %q: %s", tag, raw)
		}
	}

	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("Unmarshal returned error: %v", err)
	}
	if back.EventID != msg.EventID || back.CorrelationID != msg.CorrelationID {
		t.Errorf("round-trip identity mismatch: got %+v", back)
	}
}

func TestMessageClone(t *testing.T) {
	t.Run("variables are independent", func(t *testing.T) {
		msg, err := NewMessage("corr-1")
		if err != nil {
			t.Fatalf("NewMessage returned error: %v", err)
		}
		msg.Variables.Set("flag", true)

		clone := msg.Clone()
		clone.Variables.Set("flag", false)
		clone.Variables.Set("extra", "added")

		if got, _ := msg.Variables.Bool("flag"); !got {
			t.Error("mutating clone variables changed the original")
		}
		if _, ok := msg.Variables.String("extra"); ok {
			t.Error("adding a clone variable leaked into the original")
		}
	})

	t.Run("body is independent", func(t *testing.T) {
		msg := &Message{}
		if err := msg.SetBodyJSON([]byte(`{"k":"v"}`)); err != nil {
			t.Fatalf("SetBodyJSON returned error: %v", err)
		}

		clone := msg.Clone()
		body, ok := clone.Body.(map[string]any)
		if !ok {
			t.Fatalf("clone Body is %T, want map[string]any", clone.Body)
		}
		body["k"] = "mutated"

		original, _ := msg.Body.(map[string]any)
		if original["k"] != "v" {
			t.Errorf("mutating clone body changed the original: %v", original["k"])
		}
	})

	t.Run("body schema bytes are independent", func(t *testing.T) {
		msg := &Message{BodySchema: json.RawMessage(`{"type":"object"}`)}

		clone := msg.Clone()
		clone.BodySchema[0] = 'X'

		if msg.BodySchema[0] == 'X' {
			t.Error("mutating clone body schema changed the original")
		}
	})

	t.Run("identity fields are preserved", func(t *testing.T) {
		msg, err := NewMessage("corr-1")
		if err != nil {
			t.Fatalf("NewMessage returned error: %v", err)
		}

		clone := msg.Clone()
		if clone.EventID != msg.EventID || clone.CorrelationID != msg.CorrelationID {
			t.Errorf("clone identity mismatch: got %+v, want %+v", clone, msg)
		}
	})

	t.Run("nil body and variables do not panic", func(t *testing.T) {
		msg := &Message{EventID: "id"}

		clone := msg.Clone()
		if clone.Variables != nil || clone.Body != nil {
			t.Errorf("clone of empty message = %+v, want nil body and variables", clone)
		}
	})
}

func TestVariablesTypedAccessors(t *testing.T) {
	t.Run("native int via Set", func(t *testing.T) {
		var v Variables
		v.Set("n", 42)
		if got, ok := v.Int("n"); !ok || got != 42 {
			t.Errorf("Int(n) = (%d, %v), want (42, true)", got, ok)
		}
	})

	t.Run("json-sourced number is float64 but Int coerces", func(t *testing.T) {
		var v Variables
		if err := json.Unmarshal([]byte(`{"n":42}`), &v); err != nil {
			t.Fatalf("Unmarshal returned error: %v", err)
		}
		if _, isFloat := v["n"].(float64); !isFloat {
			t.Fatalf("JSON-decoded n is %T, want float64", v["n"])
		}
		if got, ok := v.Int("n"); !ok || got != 42 {
			t.Errorf("Int(n) = (%d, %v), want (42, true)", got, ok)
		}
	})

	t.Run("fractional float rejected by Int, accepted by Float", func(t *testing.T) {
		var v Variables
		v.Set("f", 3.5)
		if got, ok := v.Int("f"); ok || got != 0 {
			t.Errorf("Int(f) = (%d, %v), want (0, false)", got, ok)
		}
		if got, ok := v.Float("f"); !ok || got != 3.5 {
			t.Errorf("Float(f) = (%v, %v), want (3.5, true)", got, ok)
		}
	})

	t.Run("string and bool happy and wrong-type paths", func(t *testing.T) {
		var v Variables
		v.Set("s", "text")
		v.Set("b", true)
		if got, ok := v.String("s"); !ok || got != "text" {
			t.Errorf("String(s) = (%q, %v), want (text, true)", got, ok)
		}
		if got, ok := v.Bool("b"); !ok || !got {
			t.Errorf("Bool(b) = (%v, %v), want (true, true)", got, ok)
		}
		if got, ok := v.String("b"); ok || got != "" {
			t.Errorf("String(b) = (%q, %v), want (\"\", false)", got, ok)
		}
		if got, ok := v.Bool("s"); ok || got {
			t.Errorf("Bool(s) = (%v, %v), want (false, false)", got, ok)
		}
	})

	t.Run("missing key returns zero and false", func(t *testing.T) {
		var v Variables
		if got, ok := v.String("x"); ok || got != "" {
			t.Errorf("String(missing) = (%q, %v), want (\"\", false)", got, ok)
		}
		if got, ok := v.Int("x"); ok || got != 0 {
			t.Errorf("Int(missing) = (%d, %v), want (0, false)", got, ok)
		}
		if got, ok := v.Bool("x"); ok || got {
			t.Errorf("Bool(missing) = (%v, %v), want (false, false)", got, ok)
		}
		if got, ok := v.Float("x"); ok || got != 0 {
			t.Errorf("Float(missing) = (%v, %v), want (0, false)", got, ok)
		}
	})
}
