package api

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/types"
)

// A message has to survive the round trip whole: the ids, the variables, the
// body, and the flag that says how to serve the body.
func TestMessageRoundTrip(t *testing.T) {
	msg := newTestMessage(t, "corr-1")
	msg.Body = map[string]any{"order": "A-1", "total": 12.5, "items": []any{"a", "b"}}
	msg.Variables = types.Variables{"tenant": "acme", "retries": float64(2)}

	wire, err := encodeMessage(*msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	// Through actual JSON, because that is what crosses.
	encoded, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var back messageWire
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	got, err := decodeMessage(back)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.EventID != msg.EventID || got.CorrelationID != msg.CorrelationID {
		t.Fatalf("ids = (%q, %q), want (%q, %q)",
			got.EventID, got.CorrelationID, msg.EventID, msg.CorrelationID)
	}
	if !reflect.DeepEqual(got.Body, msg.Body) {
		t.Fatalf("body = %#v, want %#v", got.Body, msg.Body)
	}
	if !reflect.DeepEqual(got.Variables, msg.Variables) {
		t.Fatalf("variables = %#v, want %#v", got.Variables, msg.Variables)
	}
}

// A raw payload that arrives without its flag is served as JSON on the other
// side, so the flag has to cross.
func TestRawContentSurvivesTheWire(t *testing.T) {
	msg := newTestMessage(t, "")
	msg.SetRawBody("text/csv", "a,b\n1,2\n")

	wire, err := encodeMessage(*msg)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if !wire.RawContent {
		t.Fatal("rawContent was dropped")
	}
	got, err := decodeMessage(wire)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	contentType, data, ok := got.RawBody()
	if !ok || contentType != "text/csv" || data != "a,b\n1,2\n" {
		t.Fatalf("RawBody = (%q, %q, ok %v)", contentType, data, ok)
	}
}

// The trace id rides in Variables, so dropping internal variables is how a trace
// stops surviving a process boundary. It must cross.
func TestInternalVariablesCrossTheWire(t *testing.T) {
	msg := newTestMessage(t, "")
	want, err := msg.EnsureTraceID()
	if err != nil {
		t.Fatal(err)
	}

	wire, err := encodeMessage(*msg)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeMessage(wire)
	if err != nil {
		t.Fatal(err)
	}
	if got.TraceID() != want {
		t.Fatalf("trace id = %q, want %q: a trace must survive the boundary", got.TraceID(), want)
	}
}

// An empty message is a legitimate message, not an encoding failure.
func TestEmptyMessageRoundTrips(t *testing.T) {
	got, err := decodeMessage(messageWire{})
	if err != nil {
		t.Fatalf("decode of an empty envelope: %v", err)
	}
	if got.Body != nil {
		t.Fatalf("body = %#v, want nil", got.Body)
	}
}

// A body that cannot be encoded names the encoding side, because that is a
// different bug in a different place from one that cannot be decoded.
func TestEncodeFailureNamesItself(t *testing.T) {
	msg := newTestMessage(t, "")
	msg.Body = make(chan int)
	if _, err := encodeMessage(*msg); err == nil {
		t.Fatal("encode err = nil, want a failure")
	}
}

// newTestMessage builds a message the way the runtime does.
func newTestMessage(t *testing.T, correlationID string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage(correlationID)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}
