package api

import (
	"encoding/json"
	"fmt"

	"github.com/juancavallotti/octo/runtime/types"
)

// messageWire is how a types.Message crosses the platform API, shared by queues
// and topics.
//
// It is an explicit envelope rather than types.Message's own JSON tags for two
// reasons. Those tags are snake_case, and the rest of this contract is camelCase
// — an implementer should not have to remember which half of the document
// switches convention. And a published contract should not move because an
// internal type gained a field: this envelope names exactly what crosses, so
// adding something to types.Message is a decision here rather than an accident.
//
// Variables cross whole, internal ones included. That is deliberate and matches
// the k8s module, which ships every variable as an Octo-Var-* header: the trace
// id rides in Variables, and dropping the internal ones is how a trace stops
// surviving a process boundary.
type messageWire struct {
	EventID       string          `json:"eventId,omitempty"`
	CorrelationID string          `json:"correlationId,omitempty"`
	Variables     types.Variables `json:"variables,omitempty"`
	Body          json.RawMessage `json:"body,omitempty"`
	BodySchema    json.RawMessage `json:"bodySchema,omitempty"`
	// RawContent marks Body as carrying the raw-content shape
	// {contentType, rawData} rather than decoded JSON. It has to cross, because a
	// raw payload that arrives without it is served as JSON on the other side.
	RawContent bool `json:"rawContent,omitempty"`
}

// encodeMessage converts a message for the wire.
//
// The body is carried as raw JSON rather than as an any, so it round-trips
// through the envelope without a second decode: the platform stores and returns
// bytes it has no reason to interpret.
func encodeMessage(msg types.Message) (messageWire, error) {
	out := messageWire{
		EventID:       msg.EventID,
		CorrelationID: msg.CorrelationID,
		Variables:     msg.Variables,
		BodySchema:    msg.BodySchema,
		RawContent:    msg.RawContent,
	}
	if msg.Body != nil {
		body, err := json.Marshal(msg.Body)
		if err != nil {
			return messageWire{}, fmt.Errorf("api: encode message body: %w", err)
		}
		out.Body = body
	}
	return out, nil
}

// decodeMessage rebuilds a message from the wire.
func decodeMessage(in messageWire) (types.Message, error) {
	out := types.Message{
		EventID:       in.EventID,
		CorrelationID: in.CorrelationID,
		Variables:     in.Variables,
		BodySchema:    in.BodySchema,
		RawContent:    in.RawContent,
	}
	if len(in.Body) > 0 {
		var body any
		if err := json.Unmarshal(in.Body, &body); err != nil {
			return types.Message{}, fmt.Errorf("api: decode message body: %w", err)
		}
		out.Body = body
	}
	return out, nil
}
