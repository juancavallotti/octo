package tracing

import (
	"context"
	"encoding/json"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// flowKinds maps the flow bus's vocabulary onto the trace record's. The two are
// deliberately the same words, so this is a lookup rather than a translation, and
// a kind added to one side without the other resolves to "" and is skipped rather
// than recorded as something it is not.
var flowKinds = map[types.FlowEventKind]types.TraceKind{
	types.FlowEventStarted:   types.TraceFlowStarted,
	types.FlowEventCompleted: types.TraceFlowCompleted,
	types.FlowEventDropped:   types.TraceFlowDropped,
	types.FlowEventFailed:    types.TraceFlowFailed,
}

// blockKinds does the same for the block dispatcher's two sides.
var blockKinds = map[types.BlockEventKind]types.TraceKind{
	types.BlockPreInvoke:  types.TraceBlockPreInvoke,
	types.BlockPostInvoke: types.TraceBlockPostInvoke,
}

// onFlowEvent records the shape of one invocation: when it started, how it ended,
// how long it took, and which block failed it.
//
// It runs on the flow's own goroutine, like every bus subscriber, but only on the
// two events that bracket a whole message rather than per block — so it is the
// cheap half of tracing.
func (s *Service) onFlowEvent(event types.FlowEvent) {
	tracer := core.Tracer()
	if !tracer.Enabled() {
		return
	}
	kind, known := flowKinds[event.Kind]
	if !known {
		return
	}

	record := types.TraceEvent{
		Kind:       kind,
		TraceID:    event.TraceID,
		EventID:    event.EventID,
		Flow:       event.Flow,
		OccurredAt: event.OccurredAt,
		DurationNs: event.Duration.Nanoseconds(),
	}
	if event.Err != nil {
		record.Err = event.Err.Error()
	}
	// The failing block arrives as a label rather than an address (it names the
	// block's type and name, not its position), so it cannot go in Path, where
	// everything else is addressable.
	if event.Block != "" {
		record.Attrs = map[string]any{"block": event.Block}
	}
	if event.Result != nil {
		record.CorrelationID = event.Result.CorrelationID
		s.capture(&record, event.Result)
	}
	tracer.Publish(record)
}

// onBlockEvent records one side of one block invocation.
//
// This is the hot path: it runs inline, on the flow's goroutine, once before and
// once after every traced block of every message. Everything it needs from the
// message is taken here and now, because the pointer it is handed is live — the
// flow resumes mutating the message the instant this returns, and Variables is a
// plain map, so reading it from anywhere else is a data race that can panic the
// process.
func (s *Service) onBlockEvent(_ context.Context, event types.BlockEvent) {
	tracer := core.Tracer()
	if !tracer.Enabled() {
		return
	}
	kind, known := blockKinds[event.Kind]
	if !known {
		return
	}

	record := types.TraceEvent{
		Kind:          kind,
		EventID:       event.EventID,
		CorrelationID: event.CorrelationID,
		Flow:          event.Flow,
		Path:          event.Path,
		BlockType:     event.BlockType,
		OccurredAt:    event.OccurredAt,
		DurationNs:    event.Duration.Nanoseconds(),
		Dropped:       event.Dropped,
	}
	if event.Err != nil {
		record.Err = event.Err.Error()
	}
	if event.Message != nil {
		// The trace id is read off the message rather than carried on the event:
		// the listener already holds the message and is allowed to read it, and
		// putting the field on BlockEvent would cost every block in the process a
		// map lookup for this one consumer's benefit.
		record.TraceID = event.Message.TraceID()
		s.capture(&record, event.Message)
	}
	tracer.Publish(record)
}

// capture copies the message's payload into the record, subject to the flags and
// the size cap.
//
// It marshals here, inline, rather than keeping the message for the drain
// goroutine to serialize later. Clone is not an alternative: it copies Variables
// shallowly, so a nested value inside one stays shared with the live message and
// reading it elsewhere is the same race, just harder to find. Marshalling here
// also means the cap is applied before an oversized payload is ever queued, and
// that what leaves this function is bytes and scalars — nothing that points back
// at anything the runtime is still using.
func (s *Service) capture(record *types.TraceEvent, msg *types.Message) {
	if s.bodies {
		s.captureBody(record, msg)
	}
	if s.vars {
		s.captureVars(record, msg)
	}
}

// captureBody marshals the body, omitting it whole when it is over the cap.
//
// Whole, not truncated: a JSON document cut at a byte boundary is not JSON, and
// one of those in the stream breaks the parser for every record after it. The
// size that was refused is recorded instead, which is the part a reader actually
// needs — knowing a payload was too big to keep is useful, half a payload is not.
func (s *Service) captureBody(record *types.TraceEvent, msg *types.Message) {
	if msg.Body == nil {
		return
	}
	raw, err := json.Marshal(msg.Body)
	if err != nil {
		// A body that will not marshal is not worth failing the block over, and
		// the record is still useful without it.
		record.Truncated = true
		record.SetAttr("bodyError", err.Error())
		return
	}
	if len(raw) > s.maxPayload {
		record.Truncated = true
		record.SetAttr("bodyBytes", len(raw))
		return
	}
	record.Body = raw
}

// captureVars copies the message's variables, leaving out the runtime's own.
//
// The internal ones are excluded for the same reason Reported excludes them: they
// are bookkeeping between the engine and its blocks. The trace id is one of them,
// and it is already on the record as a field of its own.
func (s *Service) captureVars(record *types.TraceEvent, msg *types.Message) {
	if len(msg.Variables) == 0 {
		return
	}
	captured := make(map[string]any, len(msg.Variables))
	for name, value := range msg.Variables {
		if types.IsInternalVar(name) {
			continue
		}
		captured[name] = value
	}
	if len(captured) == 0 {
		return
	}
	// Variables hold arbitrary values, including ones a block put there that
	// point into structures the flow still owns. Round-tripping through JSON is
	// what turns them into a value the record can own outright — and it applies
	// the same cap the body gets, since a variable can hold a whole document.
	raw, err := json.Marshal(captured)
	if err != nil {
		record.SetAttr("varsError", err.Error())
		return
	}
	if len(raw) > s.maxPayload {
		record.Truncated = true
		record.SetAttr("varsBytes", len(raw))
		return
	}
	var owned map[string]any
	if err := json.Unmarshal(raw, &owned); err != nil {
		record.SetAttr("varsError", err.Error())
		return
	}
	record.Vars = owned
}
