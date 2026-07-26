package types

import "time"

// FlowEventKind enumerates the lifecycle outcomes published on the flow-event
// bus as a message travels through a flow.
type FlowEventKind string

const (
	// FlowEventStarted marks a message accepted onto a flow's pipeline.
	FlowEventStarted FlowEventKind = "started"
	// FlowEventCompleted marks a message that traversed the full block chain.
	FlowEventCompleted FlowEventKind = "completed"
	// FlowEventDropped marks a message a block intentionally filtered out.
	FlowEventDropped FlowEventKind = "dropped"
	// FlowEventFailed marks a message whose processing aborted with an error.
	FlowEventFailed FlowEventKind = "failed"
)

// FlowEvent is an immutable record of a single message's progress through a
// flow. It is pure data so any package (connectors, metrics) may consume it
// without depending on the core runtime.
type FlowEvent struct {
	// Kind is the lifecycle outcome this event records.
	Kind FlowEventKind
	// Flow is the name of the flow the message is travelling through.
	Flow string
	// EventID is the Message.EventID this event concerns.
	EventID string
	// OccurredAt is when the event was published.
	OccurredAt time.Time
	// Duration is how long the flow took to process the message, including its
	// error path when one ran. It is set on the three terminal events and is zero
	// on FlowEventStarted.
	//
	// It is measured where the flow runs rather than left to a subscriber pairing
	// started with terminal, for the same reason BlockEvent.Duration is: a pairing
	// subscriber needs a concurrent map keyed by EventID, and it is wrong for every
	// message still in flight when the process dies.
	//
	// A flow reached through flow-ref is its own flow and times itself, so its
	// duration nests inside the caller's rather than adding to it.
	Duration time.Duration
	// Err is set only for FlowEventFailed.
	Err error
	// Block is the label of the block the failure originated in. Set only for
	// FlowEventFailed, and empty when the error did not come from a block. When a
	// flow's error path itself fails, this is the block that failed inside the
	// error path.
	Block string
	// Result is the message at the terminal event: the flow's output for
	// FlowEventCompleted, or the input message for FlowEventDropped and
	// FlowEventFailed. It is nil for FlowEventStarted. Subscribers must treat
	// it as read-only; it lets request/response sources return the final
	// payload to a caller correlated by EventID.
	Result *Message
}

// BlockEventKind enumerates the two points a block is observed at: just before it
// is invoked, and just after it returns.
type BlockEventKind string

const (
	// BlockPreInvoke is published before a block runs, with the message it is
	// about to receive.
	BlockPreInvoke BlockEventKind = "pre-invoke"
	// BlockPostInvoke is published after a block returns, for all three outcomes:
	// a message, a drop, or a failure.
	BlockPostInvoke BlockEventKind = "post-invoke"
)

// BlockEvent records one block's invocation. Like FlowEvent it is pure data, so
// any package (connectors, metrics) may consume it without depending on the core
// runtime — but unlike FlowEvent it is emitted per block rather than per message,
// and it carries the live message rather than a result.
//
// Every field except Message is copied on the flow's own goroutine when the event
// is created, so all of them are safe to read from any goroutine. Message is not;
// see its own comment.
type BlockEvent struct {
	// Kind is which side of the invocation this event records.
	Kind BlockEventKind
	// Flow is the name of the root flow the message is travelling through, and
	// Path is the block's address within it — `orders.checkHeader[else].api-call-1`,
	// the same grammar `octo invoke --spies` accepts (see core/blockpath.go).
	//
	// Path is a label, not a resolvable handle: two blocks the address resolver
	// would call ambiguous (two unnamed blocks of the same type in one chain) mint
	// the same path here, and a name carrying a '.', '[' or ']' mints one the
	// address parser splits the wrong way.
	Flow string
	Path string
	// BlockType is the block's effective type, after any ref is resolved.
	BlockType string
	// EventID and CorrelationID identify the message, and are what join a block
	// event to the FlowEvent stream for the same message.
	EventID       string
	CorrelationID string
	// OccurredAt is when the event was created.
	OccurredAt time.Time

	// Message is the live message the block is about to receive (pre-invoke) or
	// the one it produced (post-invoke, and the input when it dropped or failed).
	// It is a reference, not a copy — which is what makes emitting one cheap.
	//
	// Listeners run on the flow's own goroutine, with nothing else touching the
	// message, so a listener may read it and may call Clone, Scoped or Reported.
	//
	// That holds only for as long as the listener is running. The flow resumes
	// mutating the message the moment the listener returns, so a listener must not
	// retain the pointer: to take contents elsewhere, Clone here and hand the copy
	// to a worker it owns. Variables is a plain map, so reading it from another
	// goroutine is a data race that can panic the process.
	Message *Message

	// Duration is how long the invocation took. Post-invoke only.
	//
	// It is measured here rather than left to a listener pairing pre with post,
	// because pairing is not reliable: a fork runs the same Path on several
	// goroutines at once, so a listener cannot tell which pre belongs to which post.
	Duration time.Duration
	// Err is the error the block failed with. Post-invoke only.
	Err error
	// Dropped reports that the block filtered the message out — it returned no
	// message and no error, and the rest of the chain was skipped. Post-invoke
	// only.
	Dropped bool
}
