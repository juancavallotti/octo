package types

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// eventIDBytes is the number of random bytes used to generate an EventID.
const eventIDBytes = 16

// errEmptyBody is returned when a body operation is attempted on a message
// whose Body has not been set.
var errEmptyBody = errors.New("message body is empty")

// Message is the first-class unit of work flowing through the processing
// pipeline. The service is JSON-only by design, so Body always holds decoded
// JSON (numbers are float64, objects map[string]any, arrays []any).
type Message struct {
	// EventID uniquely identifies this message. It is generated at
	// construction time and is stable for the life of the message.
	EventID string `json:"event_id"`

	// CorrelationID groups related messages across a logical flow. It is
	// caller-supplied and may be empty.
	CorrelationID string `json:"correlation_id,omitempty"`

	// Variables holds arbitrary per-message values keyed by name. Use the
	// typed accessors on Variables rather than asserting types directly.
	Variables Variables `json:"variables,omitempty"`

	// Body is the decoded JSON payload. Pipeline stages may mutate it in
	// place; SetBodyJSON and BodyJSON bridge to and from wire bytes.
	Body any `json:"body,omitempty"`

	// BodySchema is the JSON Schema describing Body, stored as raw JSON.
	// Validation of Body against BodySchema lives in the core module, which
	// may depend on a schema library; types stays dependency-free.
	BodySchema json.RawMessage `json:"body_schema,omitempty"`
}

// NewMessage returns a Message with a freshly generated EventID and an
// initialized Variables map. correlationID may be empty.
func NewMessage(correlationID string) (*Message, error) {
	id, err := newEventID()
	if err != nil {
		return nil, err
	}
	return &Message{
		EventID:       id,
		CorrelationID: correlationID,
		Variables:     make(Variables),
	}, nil
}

// newEventID returns a random hex-encoded identifier using crypto/rand.
func newEventID() (string, error) {
	buf := make([]byte, eventIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate event id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Clone returns a copy of the message safe for concurrent use by independent
// branches (e.g. a fork's parallel flows). The copy gets fresh Variables and
// BodySchema backing storage and a deep copy of Body via a JSON round-trip, so
// top-level mutations on the copy do not affect the original.
//
// Body is JSON-only by the type's contract, so the round-trip is well defined;
// as with SetBodyJSON it normalizes Body to decoded-JSON kinds (numbers become
// float64, objects map[string]any, arrays []any). Values stored inside Variables
// are copied shallowly, so deeply nested reference values remain shared.
func (m *Message) Clone() *Message {
	clone := *m

	if m.Variables != nil {
		clone.Variables = make(Variables, len(m.Variables))
		for k, v := range m.Variables {
			clone.Variables[k] = v
		}
	}

	if len(m.BodySchema) > 0 {
		clone.BodySchema = make(json.RawMessage, len(m.BodySchema))
		copy(clone.BodySchema, m.BodySchema)
	}

	if m.Body != nil {
		if raw, err := json.Marshal(m.Body); err == nil {
			var decoded any
			if json.Unmarshal(raw, &decoded) == nil {
				clone.Body = decoded
			}
		}
	}

	return &clone
}

// SetBodyJSON decodes raw JSON into Body. Per encoding/json rules numbers
// become float64, objects map[string]any and arrays []any.
func (m *Message) SetBodyJSON(raw []byte) error {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	m.Body = decoded
	return nil
}

// BodyJSON marshals Body back to JSON bytes, for connectors writing the
// message out or for schema validation.
func (m *Message) BodyJSON() ([]byte, error) {
	raw, err := json.Marshal(m.Body)
	if err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return raw, nil
}

// DecodeBody marshals Body and unmarshals it into target, which must be a
// non-nil pointer. It is the convenient path from a decoded body to a typed
// struct. It returns an error if Body has not been set.
func (m *Message) DecodeBody(target any) error {
	if m.Body == nil {
		return errEmptyBody
	}
	raw, err := m.BodyJSON()
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}
