package types

import (
	"encoding/json"
	"time"
)

// TraceKind enumerates what a trace record is about. The vocabulary mirrors the
// two event streams the runtime already publishes — FlowEventKind and
// BlockEventKind — rather than inventing a parallel one, so a reader that knows
// the engine's words knows these, and there is no translation table to keep in
// sync as either side grows.
type TraceKind string

const (
	// TraceFlowStarted marks a message accepted onto a flow's pipeline.
	TraceFlowStarted TraceKind = "flow.started"
	// TraceFlowCompleted marks a message that traversed the full block chain.
	TraceFlowCompleted TraceKind = "flow.completed"
	// TraceFlowDropped marks a message a block intentionally filtered out.
	TraceFlowDropped TraceKind = "flow.dropped"
	// TraceFlowFailed marks a message whose processing aborted unhandled.
	TraceFlowFailed TraceKind = "flow.failed"

	// TraceBlockPreInvoke records the message a block is about to receive.
	TraceBlockPreInvoke TraceKind = "block.pre-invoke"
	// TraceBlockPostInvoke records what a block returned: a message, a drop or
	// a failure, with how long it took.
	TraceBlockPostInvoke TraceKind = "block.post-invoke"

	// TraceLLMTurn records one model turn of any AI block — an ai-agent iteration,
	// an ai-router round, an ai-retry attempt, an ai-mapping call: the request
	// boundary both the blocking and the streaming path share, with the model that
	// served it and the usage the provider reported for it.
	TraceLLMTurn TraceKind = "llm.turn"
	// TraceLLMEmbed records one embedding call, with the size of the batch it sent,
	// the model that served it, and the tokens the provider charged.
	//
	// It is separate from TraceLLMTurn because the two are billed from different
	// rate cards and share almost no attributes — an embedding has no stop reason,
	// no tool calls and no output tokens. A reader that summed one as the other
	// would be wrong in a way the arithmetic would never reveal.
	TraceLLMEmbed TraceKind = "llm.embed"

	// TraceSourceReceive records a message admitted from an inbound request.
	TraceSourceReceive TraceKind = "source.receive"
	// TraceSourceRespond records the response a source eventually wrote for one,
	// including the ones no flow result reached: a timeout, a disconnect.
	TraceSourceRespond TraceKind = "source.respond"
	// TraceSourceEmit records a message a scheduled source produced, with no
	// caller to answer.
	TraceSourceEmit TraceKind = "source.emit"

	// TraceDropped is the in-band marker for records the publisher could not
	// keep, carrying how many were lost. A trace with holes that does not say so
	// is worse than no trace: the gap is invisible in the records that survived,
	// and every conclusion drawn from them is quietly wrong.
	TraceDropped TraceKind = "trace.dropped"
)

// TraceEvent is one record in the trace stream: pure data, like FlowEvent and
// BlockEvent, so any package may produce or consume it without depending on the
// core runtime.
//
// Unlike those two it is built to leave the process — to a file, to a broker —
// so every field is a value that survives serialization. Nothing here points at
// anything the runtime is still using: the message's payload is captured as
// bytes at the moment of the record, and an error is rendered to its text
// rather than carried as an error.
type TraceEvent struct {
	// Seq totally orders the records one publisher produced. OccurredAt is the
	// axis to read a trace on, but two records can share a timestamp, and a
	// reader that has to sort by it cannot tell a tie from a reordering. Seq
	// also makes a gap provable: consecutive records whose Seq is not
	// consecutive lost something in between.
	//
	// Seq is unique and gapless at the point of publication, but the stream is
	// not necessarily written in Seq order: a publisher stamps the number and
	// then queues the record, and a goroutine preempted between the two lets a
	// later number reach the sink first. So Seq is what a reader sorts by, not
	// something a reader may assume is already sorted.
	Seq uint64 `json:"seq"`

	// Kind is what this record is about.
	Kind TraceKind `json:"kind"`

	// TraceID is the id shared by every record of one end-to-end invocation,
	// including the sub-invocations a flow-ref, split or aggregate creates —
	// each of which has an EventID of its own. It is what a reader groups on.
	TraceID string `json:"traceId,omitempty"`

	// EventID identifies the single message this record concerns, and
	// CorrelationID is the caller's own id when it supplied one. Together with
	// TraceID they are what join a trace record to the FlowEvent and BlockEvent
	// streams for the same message.
	EventID       string `json:"eventId,omitempty"`
	CorrelationID string `json:"correlationId,omitempty"`

	// Flow is the root flow the message is travelling through and Path is the
	// block's address within it — `orders.checkHeader[else].api-call-1`, the
	// grammar core/blockpath.go defines. Path is empty for records that are not
	// about a block.
	Flow      string `json:"flow,omitempty"`
	Path      string `json:"path,omitempty"`
	BlockType string `json:"blockType,omitempty"`

	// OccurredAt is when the traced thing happened, not when it was written:
	// the record is captured inline and drained later.
	OccurredAt time.Time `json:"at"`

	// DurationNs is how long the traced thing took, in nanoseconds. It is a
	// plain integer rather than a time.Duration so the wire format is decided
	// once here rather than per sink. Zero for records that mark a point rather
	// than an interval.
	DurationNs int64 `json:"durationNs,omitempty"`

	// Err is the failure's message, rendered where the record was captured. It
	// is a string rather than an error because an error is a pointer into a
	// live value graph nobody guarantees is serializable — and rendering it
	// here is what keeps "this record retains nothing" true.
	Err string `json:"error,omitempty"`

	// Dropped reports that a block filtered the message out: it returned no
	// message and no error, and the rest of the chain was skipped.
	Dropped bool `json:"dropped,omitempty"`

	// Body is the message's payload, marshalled at capture. It is nil when
	// payload capture is off, when the message had no body, or when the body
	// was over the configured cap — see Truncated.
	Body json.RawMessage `json:"body,omitempty"`

	// Vars is the message's variables at capture, minus the runtime's internal
	// ones (see IsInternalVar). Nil when variable capture is off.
	Vars map[string]any `json:"vars,omitempty"`

	// Truncated reports that a payload was dropped for exceeding the cap. The
	// payload is omitted whole rather than cut short: a sliced JSON document is
	// not JSON, and one in the stream poisons every reader downstream of it.
	// Attrs carries the size that was refused.
	Truncated bool `json:"truncated,omitempty"`

	// Attrs carries whatever is specific to this record's kind — the model and
	// token usage of an llm.turn, the status of a source.respond, the count of
	// a trace.dropped. Keeping them here rather than as columns is what lets a
	// new emitter add a record without every consumer learning a new field.
	Attrs map[string]any `json:"attrs,omitempty"`
}

// SetAttr adds one kind-specific attribute, allocating the map on first use so an
// emitter that has nothing to add costs nothing. Most records carry no attributes
// at all, which is why the map is not allocated up front.
func (e *TraceEvent) SetAttr(name string, value any) {
	if e.Attrs == nil {
		e.Attrs = make(map[string]any, 1)
	}
	e.Attrs[name] = value
}
