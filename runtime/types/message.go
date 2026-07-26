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

// rawContentTypeKey and rawDataKey are the two keys a raw-content Body carries:
// the payload's MIME type and the payload itself (a UTF-8 string).
const (
	rawContentTypeKey = "contentType"
	rawDataKey        = "rawData"
)

// stopVar is the reserved Variables key a filter block sets to request that the
// flow stop after it returns, keeping the response it configured. The double
// underscore marks it internal; sinks serialize Body, not Variables, so it never
// leaks into a response. Read/write it only through RequestStop/StopRequested.
const stopVar = "__octoStop"

// Message is the first-class unit of work flowing through the processing
// pipeline. The service is JSON-only by default, so Body normally holds decoded
// JSON (numbers are float64, objects map[string]any, arrays []any).
//
// A message may opt out of that contract by setting RawContent: Body then holds
// the raw-content shape {contentType, rawData}, letting raw-aware connectors
// serve a typed non-JSON payload (see SetRawBody/RawBody).
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

	// Body is the decoded JSON payload. It is replace-only: a pipeline stage
	// assigns a new value to Body rather than writing into the value already
	// there, because a message may share its body with a scoped copy (see
	// Scoped). Assigning Body transfers exclusive ownership of that value to
	// this message, so never hand the same value to two live messages. A stage
	// that must write in place goes through MutableBody first. SetBodyJSON and
	// BodyJSON bridge to and from wire bytes.
	Body any `json:"body,omitempty"`

	// BodySchema is the JSON Schema describing Body, stored as raw JSON.
	// Validation of Body against BodySchema lives in the core module, which
	// may depend on a schema library; types stays dependency-free.
	BodySchema json.RawMessage `json:"body_schema,omitempty"`

	// RawContent marks Body as carrying a raw, non-JSON payload in the shape
	// {contentType, rawData}. Raw-aware connectors (e.g. the http source) serve
	// rawData with the given MIME type instead of JSON-encoding Body. Defaults
	// to false; JSON remains the contract for every message that does not opt in.
	RawContent bool `json:"raw_content,omitempty"`

	// bodyShared reports that Body points at a value another Message also points
	// at, so mutating it in place would be visible there. MutableBody clears this
	// after the first copy attempt, so it means "copy-on-write still pending", not
	// "the entire body is now alias-free".
	bodyShared bool

	// bodyAliased reports that the last copy attempt left uncopyable leaves shared
	// (copyBody returned copied=false). It records residual aliasing separately
	// from bodyShared so MutableBody does not repeat full-body traversal.
	bodyAliased bool
}

// SetRawBody puts the message into raw-content mode: Body becomes the shape
// {contentType, rawData} and RawContent is set true. rawData is a UTF-8 string
// written verbatim by raw-aware sinks (e.g. the http source's response writer).
func (m *Message) SetRawBody(contentType, rawData string) {
	m.Body = map[string]any{
		rawContentTypeKey: contentType,
		rawDataKey:        rawData,
	}
	m.RawContent = true
}

// RawBody returns the contentType and rawData when the message is in raw-content
// mode and Body matches the expected shape; ok is false otherwise. Body may have
// been rebuilt from JSON (e.g. after Clone or a wire round-trip), so the keys are
// asserted defensively out of a map[string]any.
func (m *Message) RawBody() (contentType, rawData string, ok bool) {
	if !m.RawContent {
		return "", "", false
	}
	fields, ok := m.Body.(map[string]any)
	if !ok {
		return "", "", false
	}
	contentType, ok = fields[rawContentTypeKey].(string)
	if !ok {
		return "", "", false
	}
	rawData, ok = fields[rawDataKey].(string)
	if !ok {
		return "", "", false
	}
	return contentType, rawData, true
}

// RequestStop marks the message so the flow engine stops running further blocks
// once the current block returns, completing the flow with the message as-is
// (its configured body/status). It is the primitive behind "filter" blocks that
// terminate a flow and shape their own response. The flag rides in Variables, so
// it bubbles up through nested composite sub-flows and folds back across flow-ref
// boundaries automatically.
func (m *Message) RequestStop() { m.Variables.Set(stopVar, true) }

// StopRequested reports whether a block has called RequestStop on this message.
// The flow engine checks it after each block to short-circuit the chain.
func (m *Message) StopRequested() bool {
	stop, _ := m.Variables.Bool(stopVar)
	return stop
}

// Reported returns a copy of the message with the runtime's internal variables
// removed: the shape to show a user. It is what a caller that serializes a whole
// message for human eyes — `octo invoke`, the CLI's debug envelope — should print.
//
// The only such variable today is the stop flag, which a filter block sets to end
// the flow. It is bookkeeping between the engine and its blocks, so reporting a
// filtered flow's message as though it carried a variable the flow set itself would
// be a lie. Variables the flow really set are untouched.
//
// It scopes rather than clones: only Variables are edited, so deep-copying an
// entire result body to delete one key would be pure waste on the way to a
// caller that is about to serialize it.
func (m *Message) Reported() *Message {
	reported := m.Scoped()
	delete(reported.Variables, stopVar)
	return reported
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

// Rekey assigns the message a freshly generated EventID, returning the new ID.
// It is used when a message is forwarded into another flow (e.g. by the flow-ref
// block) so the sub-invocation correlates on its own ID rather than colliding
// with the originating flow's terminal event, which keys on the original EventID.
func (m *Message) Rekey() (string, error) {
	id, err := newEventID()
	if err != nil {
		return "", err
	}
	m.EventID = id
	return id, nil
}

// newEventID returns a random hex-encoded identifier using crypto/rand.
func newEventID() (string, error) {
	buf := make([]byte, eventIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate event id: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// Clone returns a copy of the message that shares nothing mutable with it: fresh
// Variables and BodySchema backing storage and a deep copy of Body. It is the copy
// to take whenever the result outlives the caller's frame or leaves its goroutine
// — a fork branch, a flow-ref, a queue or event publish, a debug snapshot. Use
// Scoped instead for a sub-flow that runs to completion before the original is
// touched again; it is the same isolation without the body copy.
//
// Body is JSON-only by the type's contract, so the copy normalizes it to
// decoded-JSON kinds (numbers become float64, objects map[string]any, arrays
// []any) — see copyBody. Values stored inside Variables are copied shallowly, so
// deeply nested reference values remain shared.
//
// RawContent is copied by the shallow struct copy, and a raw-content Body survives
// intact because its rawData is a string, not raw bytes: the {contentType,
// rawData} map copies to the same shape.
func (m *Message) Clone() *Message {
	clone := m.shallow()
	body, copied := copyBody(m.Body)
	clone.Body = body
	// The clone owns this body outright, so nothing is pending — clear the flag
	// rather than inherit it from a receiver that happens to be in a scope pair.
	clone.bodyShared = false
	// Some bodies cannot be copied all the way down; keep that signal, but this is
	// not a pending copy-on-write share with the source message.
	clone.bodyAliased = !copied
	return clone
}

// Scoped returns a copy for a sub-flow that runs to completion on this goroutine
// before the receiver is touched again — a foreach map iteration, an enrich scope.
// It is Clone without the body deep-copy: the sub-flow gets its own Variables, so
// a variable it sets cannot leak and a loop variable cannot escape, but both
// messages read the same Body.
//
// That is what keeps mapping a collection linear in its size. In map mode the body
// IS the collection, so deep-copying it per element copied the whole collection
// once per element.
//
// Sharing is invisible because Body is replace-only (see the field's doc): a block
// rebinds Body on its own message and the other side never sees it. A block that
// must mutate the body in place calls MutableBody first, which copies out. Both
// messages are marked, so it does not matter which one mutates.
//
// Note that this marks the receiver too, which is unusual for a copying method: it
// is what makes the sharing symmetric. Use Clone, not Scoped, whenever the copy
// leaves this goroutine — sharing is only safe while one goroutine can touch
// either side.
func (m *Message) Scoped() *Message {
	scoped := m.shallow()
	m.bodyShared = true
	scoped.bodyShared = true
	return scoped
}

// MutableBody returns Body for mutation in place, first replacing it with a
// private deep copy when the body is shared with another message (see Scoped). It
// is the only supported way to write into a body rather than replace it.
//
// The copy happens at most once per message: afterwards this message owns its
// top-level body and later calls hand back the same value. If copying fails for
// some leaves, bodyAliased records that they remain shared and returned as-is —
// the caller mutates at its own risk, exactly as it would have before, since
// there is no copy to be had.
func (m *Message) MutableBody() any {
	if m.bodyShared {
		body, copied := copyBody(m.Body)
		m.Body = body
		m.bodyShared = false
		m.bodyAliased = !copied
	}
	return m.Body
}

// shallow returns a copy of the message that still points at the receiver's Body:
// the struct fields, a fresh Variables map and fresh BodySchema bytes. It is the
// common core of the copying constructors, none of which leaves a body shared
// without recording that it did.
func (m *Message) shallow() *Message {
	out := *m

	if m.Variables != nil {
		out.Variables = make(Variables, len(m.Variables))
		for k, v := range m.Variables {
			out.Variables[k] = v
		}
	}

	if len(m.BodySchema) > 0 {
		out.BodySchema = make(json.RawMessage, len(m.BodySchema))
		copy(out.BodySchema, m.BodySchema)
	}

	return &out
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
