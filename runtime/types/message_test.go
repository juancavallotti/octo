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

	// A body that breaks the JSON-only contract cannot be copied at all. Handing
	// back the original beats losing it, and the clone records residual aliasing
	// separately from pending copy-on-write.
	t.Run("a body that will not round-trip records residual aliasing", func(t *testing.T) {
		msg := &Message{Body: make(chan int)}

		clone := msg.Clone()
		if clone.Body == nil {
			t.Fatal("clone dropped a body it could not copy")
		}
		if clone.bodyShared {
			t.Error("clone should not keep copy-on-write pending")
		}
		if !clone.bodyAliased {
			t.Error("clone did not record residual aliasing")
		}
	})

	// Scoped marks the message it is called on, and every message the runtime maps,
	// enriches or reports carries that mark afterwards. A clone of one still owns
	// its body outright, so it must not inherit a copy-on-write that is not pending
	// — otherwise its first MutableBody re-copies a body nobody else can see.
	t.Run("a clone of a scoped message owns its body", func(t *testing.T) {
		msg := &Message{Body: map[string]any{"k": "v"}}
		_ = msg.Scoped()

		clone := msg.Clone()
		if clone.bodyShared {
			t.Error("clone inherited a pending copy-on-write it does not need")
		}
		before := containerIdentity(t, clone.Body)
		if containerIdentity(t, clone.MutableBody()) != before {
			t.Error("MutableBody re-copied a body the clone already owned")
		}
	})
}

func TestMessageScoped(t *testing.T) {
	// The point of the whole exercise: a scope costs no body copy.
	t.Run("the body is shared with the original", func(t *testing.T) {
		msg := &Message{Body: map[string]any{"k": "v"}}

		scoped := msg.Scoped()

		if containerIdentity(t, scoped.Body) != containerIdentity(t, msg.Body) {
			t.Error("Scoped copied the body instead of sharing it")
		}
	})

	t.Run("variables are independent", func(t *testing.T) {
		msg, err := NewMessage("corr-1")
		if err != nil {
			t.Fatalf("NewMessage returned error: %v", err)
		}
		msg.Variables.Set("flag", true)

		scoped := msg.Scoped()
		scoped.Variables.Set("flag", false)
		scoped.Variables.Set("extra", "added")

		if got, _ := msg.Variables.Bool("flag"); !got {
			t.Error("mutating scoped variables changed the original")
		}
		if _, ok := msg.Variables.String("extra"); ok {
			t.Error("adding a scoped variable leaked into the original")
		}
	})

	t.Run("body schema bytes are independent", func(t *testing.T) {
		msg := &Message{BodySchema: json.RawMessage(`{"type":"object"}`)}

		scoped := msg.Scoped()
		scoped.BodySchema[0] = 'X'

		if msg.BodySchema[0] == 'X' {
			t.Error("mutating the scoped body schema changed the original")
		}
	})

	t.Run("identity fields are preserved", func(t *testing.T) {
		msg, err := NewMessage("corr-1")
		if err != nil {
			t.Fatalf("NewMessage returned error: %v", err)
		}

		scoped := msg.Scoped()
		if scoped.EventID != msg.EventID || scoped.CorrelationID != msg.CorrelationID {
			t.Errorf("scoped identity mismatch: got %+v, want %+v", scoped, msg)
		}
	})

	t.Run("MutableBody copies a shared body before it is written", func(t *testing.T) {
		msg := &Message{Body: map[string]any{"k": "v"}}
		scoped := msg.Scoped()

		scoped.MutableBody().(map[string]any)["k"] = "mutated"

		if got := msg.Body.(map[string]any)["k"]; got != "v" {
			t.Errorf("writing through the scope's MutableBody changed the original: %v", got)
		}
		if containerIdentity(t, scoped.Body) == containerIdentity(t, msg.Body) {
			t.Error("MutableBody left the body shared")
		}
	})

	// Scoped marks both sides, so it does not matter which one writes first.
	t.Run("MutableBody on the original copies too", func(t *testing.T) {
		msg := &Message{Body: map[string]any{"k": "v"}}
		scoped := msg.Scoped()

		msg.MutableBody().(map[string]any)["k"] = "mutated"

		if got := scoped.Body.(map[string]any)["k"]; got != "v" {
			t.Errorf("writing through the original's MutableBody changed the scope: %v", got)
		}
	})

	t.Run("MutableBody hands back a body this message already owns", func(t *testing.T) {
		msg := &Message{Body: map[string]any{"k": "v"}}

		before := containerIdentity(t, msg.Body)
		if containerIdentity(t, msg.MutableBody()) != before {
			t.Error("MutableBody copied a body that was never shared")
		}

		scoped := msg.Scoped()
		first := containerIdentity(t, scoped.MutableBody())
		if containerIdentity(t, scoped.MutableBody()) != first {
			t.Error("a second MutableBody copied again")
		}
	})

	t.Run("partial copy is attempted once and aliasing is tracked", func(t *testing.T) {
		msg := &Message{Body: map[string]any{"ok": "v", "bad": make(chan int)}}
		scoped := msg.Scoped()

		first := containerIdentity(t, scoped.MutableBody())
		if !scoped.bodyAliased {
			t.Error("MutableBody did not record residual aliasing after partial copy")
		}
		if scoped.bodyShared {
			t.Error("MutableBody left copy-on-write pending after the first copy attempt")
		}

		if containerIdentity(t, scoped.MutableBody()) != first {
			t.Error("a second MutableBody recopied after a partial copy")
		}
	})

	t.Run("nil body and variables do not panic", func(t *testing.T) {
		msg := &Message{EventID: "id"}

		scoped := msg.Scoped()
		if scoped.Variables != nil || scoped.Body != nil {
			t.Errorf("scope of empty message = %+v, want nil body and variables", scoped)
		}
		if scoped.MutableBody() != nil {
			t.Error("MutableBody invented a body")
		}
	})
}

func TestReported(t *testing.T) {
	t.Run("hides the stop flag, keeps the flow's own variables", func(t *testing.T) {
		msg, err := NewMessage("")
		if err != nil {
			t.Fatalf("NewMessage returned error: %v", err)
		}
		msg.Variables.Set("tier", "gold")
		msg.RequestStop()

		reported := msg.Reported()
		if _, leaked := reported.Variables[stopVar]; leaked {
			t.Errorf("reported variables leaked %s: %v", stopVar, reported.Variables)
		}
		if tier, _ := reported.Variables.String("tier"); tier != "gold" {
			t.Errorf("reported variables = %v, want tier=gold kept", reported.Variables)
		}
	})

	t.Run("leaves the original message alone", func(t *testing.T) {
		msg, err := NewMessage("")
		if err != nil {
			t.Fatalf("NewMessage returned error: %v", err)
		}
		msg.RequestStop()

		_ = msg.Reported()

		if !msg.StopRequested() {
			t.Error("Reported cleared the stop flag on the message it was called on")
		}
	})

	// It edits variables only, so it has no reason to copy a result body that the
	// caller is about to serialize.
	t.Run("does not copy the body", func(t *testing.T) {
		msg := &Message{Body: map[string]any{"k": "v"}}

		reported := msg.Reported()

		if containerIdentity(t, reported.Body) != containerIdentity(t, msg.Body) {
			t.Error("Reported deep-copied the body to delete a variable")
		}
	})

	t.Run("nil variables do not panic", func(t *testing.T) {
		msg := &Message{EventID: "id"}

		if reported := msg.Reported(); reported.EventID != "id" {
			t.Errorf("reported = %+v, want the event id carried through", reported)
		}
	})
}

func TestRawBody(t *testing.T) {
	t.Run("SetRawBody and RawBody round-trip", func(t *testing.T) {
		msg := &Message{}
		msg.SetRawBody("text/html", "<h1>hi</h1>")

		if !msg.RawContent {
			t.Error("SetRawBody did not set RawContent")
		}
		ct, data, ok := msg.RawBody()
		if !ok {
			t.Fatal("RawBody ok = false, want true")
		}
		if ct != "text/html" || data != "<h1>hi</h1>" {
			t.Errorf("RawBody = (%q, %q), want (text/html, <h1>hi</h1>)", ct, data)
		}
	})

	t.Run("normal JSON message is not raw", func(t *testing.T) {
		msg := &Message{}
		if err := msg.SetBodyJSON([]byte(`{"contentType":"x","rawData":"y"}`)); err != nil {
			t.Fatalf("SetBodyJSON returned error: %v", err)
		}
		if _, _, ok := msg.RawBody(); ok {
			t.Error("RawBody ok = true for a message without RawContent set")
		}
	})

	t.Run("RawContent set but body shape wrong", func(t *testing.T) {
		msg := &Message{RawContent: true, Body: "not a map"}
		if _, _, ok := msg.RawBody(); ok {
			t.Error("RawBody ok = true for a malformed raw body")
		}
	})

	t.Run("survives Clone and JSON round-trip", func(t *testing.T) {
		msg := &Message{}
		msg.SetRawBody("text/csv", "a,b\n1,2")

		clone := msg.Clone()
		if !clone.RawContent {
			t.Error("Clone dropped RawContent")
		}
		ct, data, ok := clone.RawBody()
		if !ok || ct != "text/csv" || data != "a,b\n1,2" {
			t.Errorf("clone RawBody = (%q, %q, %v), want (text/csv, a,b\\n1,2, true)", ct, data, ok)
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
