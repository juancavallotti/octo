package types

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// eventIDBytes is the number of random bytes used to generate an EventID.
const eventIDBytes = 16

// errEmptyBody is returned when a body operation is attempted on a message
// whose Body has not been set.
var errEmptyBody = errors.New("message body is empty")

// The keys a raw-content Body carries: the payload's MIME type, the payload itself
// (a UTF-8 string), and — for a multipart payload — its decoded parts.
//
// RawPartsKey is optional and purely additive: it rides alongside an untouched
// rawData rather than replacing it, so a raw-aware sink serves the exact bytes it
// always did and a signature computed over the body still verifies. Only a
// multipart payload has it.
//
// They are exported because the raw-body shape is a contract shared with the
// expression engine, which reads a body's payload to decode it. Two copies of
// these names in two packages is one rename away from a silent mismatch.
const (
	RawContentTypeKey = "contentType"
	RawDataKey        = "rawData"
	RawPartsKey       = "parts"
)

// internalVarPrefix marks a Variables key as the runtime's own bookkeeping
// rather than something a flow set. Reported strips every key carrying it, so
// none of them reaches a user-facing dump of a message, and the prefix doubles as
// a reservation: a flow author's variable never collides with one of these, and
// neither does a variable arriving over the wire, where variables travel as
// headers named after their key.
const internalVarPrefix = "__"

// IsInternalVar reports whether a variable name is the runtime's own bookkeeping.
// It is exported because the trace listeners capture a message's variables and
// must leave these out for the same reason Reported does.
func IsInternalVar(name string) bool {
	return strings.HasPrefix(name, internalVarPrefix)
}

// stopVar is the reserved Variables key a filter block sets to request that the
// flow stop after it returns, keeping the response it configured. The double
// underscore marks it internal; sinks serialize Body, not Variables, so it never
// leaks into a response. Read/write it only through RequestStop/StopRequested.
const stopVar = internalVarPrefix + "octoStop"

// traceVar is the reserved Variables key carrying the id shared by every message
// of one end-to-end invocation. It exists only while tracing is enabled, so no
// flow may depend on it — which is the other reason it is internal rather than
// user-visible: a variable that is present on Tuesday and absent on Wednesday is
// not something to write a CEL expression against.
//
// It rides in Variables rather than in a field of its own so it propagates for
// free everywhere a message already goes: Clone and Scoped copy it, Rekey leaves
// it alone (a sub-invocation gets a new EventID but stays in the same trace), and
// the queue and topic encoders ship it as an Octo-Var-* header, which is what
// makes a trace survive crossing a process boundary.
//
// Read/write it only through TraceID/EnsureTraceID.
const traceVar = internalVarPrefix + "octoTraceId"

// Message is the first-class unit of work flowing through the processing
// pipeline. The service is JSON-only by default, so Body normally holds decoded
// JSON (numbers are float64, objects map[string]any, arrays []any).
//
// A message may opt out of that contract by setting RawContent: Body then holds
// the raw-content shape {contentType, rawData}, letting raw-aware connectors
// serve a typed non-JSON payload (see SetRawBody/RawBody). A multipart payload
// carries a third key, parts, holding the decoded parts by name
// (see SetRawBodyParts).
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
	// {contentType, rawData}, optionally with a third key, parts, when the payload
	// is multipart. Raw-aware connectors (e.g. the http source) serve rawData with
	// the given MIME type instead of JSON-encoding Body. Defaults to false; JSON
	// remains the contract for every message that does not opt in.
	RawContent bool `json:"raw_content,omitempty"`

	// bodyShared reports that a copy-on-write of Body is still pending: another
	// Message points at the same value, so writing into it in place would be
	// visible there. MutableBody clears it after its one copy, and Scoped sets it
	// on both sides. It is unexported and untagged, so encoding/json ignores it
	// and a decoded message gets the zero value, false — correct, since it owns
	// its body outright. The polarity is deliberate: a stale true costs one wasted
	// copy, while only a stale false would be unsafe.
	bodyShared bool
}

// SetRawBody puts the message into raw-content mode: Body becomes the shape
// {contentType, rawData} and RawContent is set true. rawData is a UTF-8 string
// written verbatim by raw-aware sinks (e.g. the http source's response writer).
func (m *Message) SetRawBody(contentType, rawData string) {
	m.Body = map[string]any{
		RawContentTypeKey: contentType,
		RawDataKey:        rawData,
	}
	m.RawContent = true
}

// SetRawBodyParts puts the message into raw-content mode with the decoded parts of
// a multipart payload alongside the raw payload itself, keyed by part name.
//
// rawData stays exactly what was received. That is the point of adding a key
// rather than replacing the body: a flow verifying a signature over the raw bytes
// and a flow reading body.parts.avatar are the same flow, and neither has to
// choose. RawBody keeps working untouched, since it reads its two keys out of the
// map and ignores anything else.
func (m *Message) SetRawBodyParts(contentType, rawData string, parts map[string]any) {
	m.SetRawBody(contentType, rawData)
	body, ok := m.Body.(map[string]any)
	if !ok {
		return
	}
	body[RawPartsKey] = parts
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
	contentType, ok = fields[RawContentTypeKey].(string)
	if !ok {
		return "", "", false
	}
	rawData, ok = fields[RawDataKey].(string)
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
// Those variables are bookkeeping between the engine and its blocks: the stop
// flag a filter block sets to end the flow, the trace id tracing rides on.
// Reporting a message as though it carried a variable the flow set itself would
// be a lie, and in the trace id's case it would also be an unstable one, since
// the variable is there only when tracing is on. Variables the flow really set
// are untouched.
//
// It strips by prefix rather than by name so a new internal variable is covered
// the moment it is named, instead of leaking until someone remembers this
// function exists.
//
// It scopes rather than clones: only Variables are edited, so deep-copying an
// entire result body to delete one key would be pure waste on the way to a
// caller that is about to serialize it.
func (m *Message) Reported() *Message {
	reported := m.Scoped()
	for name := range reported.Variables {
		if IsInternalVar(name) {
			delete(reported.Variables, name)
		}
	}
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

// TraceID returns the id of the end-to-end invocation this message belongs to,
// or "" when tracing is not running. Unlike EventID it survives Rekey, so a
// flow-ref sub-invocation, a split element and an aggregate's release all report
// the id of the message the work started from.
func (m *Message) TraceID() string {
	id, _ := m.Variables.String(traceVar)
	return id
}

// EnsureTraceID returns the message's trace id, generating one if it has none.
// An id already on the message — set by the source that admitted it, or carried
// in from another process — is kept, so whoever saw the work first names the
// trace and everything downstream joins it rather than starting its own.
func (m *Message) EnsureTraceID() (string, error) {
	if id := m.TraceID(); id != "" {
		return id, nil
	}
	id, err := newEventID()
	if err != nil {
		return "", err
	}
	m.Variables.Set(traceVar, id)
	return id, nil
}

// SetTraceID puts an existing trace id on the message, for a caller joining work
// to a trace that started elsewhere. Prefer EnsureTraceID, which will not
// overwrite an id the message already has.
func (m *Message) SetTraceID(id string) {
	if id == "" {
		return
	}
	m.Variables.Set(traceVar, id)
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
	clone.Body = copyBody(m.Body)
	// The clone owns this body outright, so nothing is pending — clear the flag
	// rather than inherit it from a receiver that happens to be in a scope pair.
	clone.bodyShared = false
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
// The copy happens at most once per message — a body copyBody cannot fully copy
// would never copy on a retry either, so a second call hands back the same value
// rather than walking it again. Any part that could not be copied stays shared
// and is returned as-is; the caller mutates it at its own risk, exactly as it
// would have before, since there is no copy to be had.
func (m *Message) MutableBody() any {
	if m.bodyShared {
		m.Body = copyBody(m.Body)
		m.bodyShared = false
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

// SetBody replaces Body with an ordinary JSON-native value, leaving raw-content
// mode if the message was in it. It is how a stage replaces the body: assigning
// the field directly sets the payload without retracting the claim RawContent
// makes about it, and a message whose body is no longer {contentType, rawData}
// while RawContent still says it is has a flag that lies about it.
//
// Nothing downstream is fooled — RawBody re-checks the shape, so a raw-aware sink
// falls back to JSON either way — but the flag is serialized (the invoke
// envelope, the queue's Octo-Raw-Content header, a trace record), and a message
// that reports raw content while carrying an object is a thing someone has to
// stop and work out.
//
// Direct assignment is left for the two places that own the flag themselves:
// SetRawBody, which sets both, and a decoder restoring a message whose
// RawContent arrived with it.
func (m *Message) SetBody(value any) {
	m.Body = value
	m.RawContent = false
}

// SetBodyJSON decodes raw JSON into Body. Per encoding/json rules numbers
// become float64, objects map[string]any and arrays []any. Like SetBody, it
// leaves raw-content mode: what it decoded is a JSON body by definition.
func (m *Message) SetBodyJSON(raw []byte) error {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	m.SetBody(decoded)
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
