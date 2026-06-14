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
	// Block is the type or name of the block where the event originated. It is
	// empty for source-level events.
	Block string
	// EventID is the Message.EventID this event concerns.
	EventID string
	// OccurredAt is when the event was published.
	OccurredAt time.Time
	// Err is set only for FlowEventFailed.
	Err error
}
