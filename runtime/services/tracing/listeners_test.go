package tracing

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// collector is an enabled publisher that keeps what it is given, so a test can
// assert on the record the listener built.
type collector struct {
	mu      sync.Mutex
	records []types.TraceEvent
}

func (c *collector) Enabled() bool { return true }

func (c *collector) Publish(event types.TraceEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, event)
}

func (c *collector) only(t *testing.T) types.TraceEvent {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.records) != 1 {
		t.Fatalf("records = %d, want 1", len(c.records))
	}
	return c.records[0]
}

// collecting installs a collector as the process tracer for one test.
func collecting(t *testing.T) *collector {
	t.Helper()
	c := &collector{}
	previous := core.Tracer()
	core.SetTracer(c)
	t.Cleanup(func() { core.SetTracer(previous) })
	return c
}

// capturingService is a service configured the way the flags default: payloads
// on, with a cap.
func capturingService() *Service {
	return &Service{enabled: true, bodies: true, vars: true, maxPayload: defaultMaxPayload}
}

func traceMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("corr-1")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	msg.Body = body
	if _, err := msg.EnsureTraceID(); err != nil {
		t.Fatalf("ensure trace id: %v", err)
	}
	return msg
}

func blockEvent(msg *types.Message) types.BlockEvent {
	return types.BlockEvent{
		Kind:          types.BlockPostInvoke,
		Flow:          "orders",
		Path:          "orders.charge",
		BlockType:     "rest",
		EventID:       msg.EventID,
		CorrelationID: msg.CorrelationID,
		OccurredAt:    time.Now(),
		Duration:      3 * time.Millisecond,
		Message:       msg,
	}
}

func TestBlockListenerRecordsTheInvocation(t *testing.T) {
	c := collecting(t)
	s := capturingService()
	msg := traceMessage(t, map[string]any{"amount": float64(12)})

	s.onBlockEvent(context.Background(), blockEvent(msg))

	record := c.only(t)
	if record.Kind != types.TraceBlockPostInvoke {
		t.Errorf("Kind = %s, want %s", record.Kind, types.TraceBlockPostInvoke)
	}
	if record.Path != "orders.charge" || record.Flow != "orders" || record.BlockType != "rest" {
		t.Errorf("address = %s/%s/%s", record.Flow, record.Path, record.BlockType)
	}
	if record.TraceID != msg.TraceID() {
		t.Errorf("TraceID = %q, want %q", record.TraceID, msg.TraceID())
	}
	if record.DurationNs != (3 * time.Millisecond).Nanoseconds() {
		t.Errorf("DurationNs = %d", record.DurationNs)
	}
	if string(record.Body) != `{"amount":12}` {
		t.Errorf("Body = %s", record.Body)
	}
}

// The record must be a value, not a view. The listener is handed a live pointer
// and the flow resumes mutating it the instant the listener returns, so anything
// the record still shared with the message would be read on another goroutine
// while the flow writes it — a race that can panic the process.
func TestBlockListenerSnapshotsRatherThanReferences(t *testing.T) {
	c := collecting(t)
	s := capturingService()
	msg := traceMessage(t, map[string]any{"amount": float64(12)})
	msg.Variables.Set("httpStatus", 200)

	s.onBlockEvent(context.Background(), blockEvent(msg))
	record := c.only(t)

	// Whatever the flow does next must not be visible in the record.
	msg.Body = map[string]any{"amount": float64(999)}
	msg.Variables.Set("httpStatus", 500)
	msg.Variables.Set("addedLater", true)

	if string(record.Body) != `{"amount":12}` {
		t.Errorf("Body followed the message: %s", record.Body)
	}
	if got := record.Vars["httpStatus"]; got != float64(200) {
		t.Errorf("Vars followed the message: httpStatus = %v", got)
	}
	if _, ok := record.Vars["addedLater"]; ok {
		t.Error("Vars picked up a variable set after the listener returned")
	}
}

// The trace id is on the record as a field of its own; carrying it again in the
// variables would be noise, and the rest of the runtime's bookkeeping is not the
// flow's data either.
func TestBlockListenerOmitsInternalVariables(t *testing.T) {
	c := collecting(t)
	s := capturingService()
	msg := traceMessage(t, nil)
	msg.RequestStop()
	msg.Variables.Set("httpStatus", 200)

	s.onBlockEvent(context.Background(), blockEvent(msg))
	record := c.only(t)

	for name := range record.Vars {
		if types.IsInternalVar(name) {
			t.Errorf("captured the internal variable %q", name)
		}
	}
	if _, ok := record.Vars["httpStatus"]; !ok {
		t.Error("dropped a variable the flow actually set")
	}
	if record.TraceID == "" {
		t.Error("the trace id did not make it onto the record")
	}
}

func TestPayloadCaptureCanBeTurnedOff(t *testing.T) {
	c := collecting(t)
	s := &Service{enabled: true, maxPayload: defaultMaxPayload}
	msg := traceMessage(t, map[string]any{"secret": "s3cret"})
	msg.Variables.Set("authorization", "Bearer abc")

	s.onBlockEvent(context.Background(), blockEvent(msg))
	record := c.only(t)

	if record.Body != nil {
		t.Errorf("Body = %s, want nothing captured", record.Body)
	}
	if record.Vars != nil {
		t.Errorf("Vars = %v, want nothing captured", record.Vars)
	}
	// The sequence is still fully recorded — that is what makes turning payloads
	// off a usable setting rather than a way to get nothing.
	if record.Path != "orders.charge" || record.TraceID == "" {
		t.Error("turning payloads off cost the record its identity")
	}
}

// An over-cap payload is omitted whole rather than cut. A JSON document sliced at
// a byte boundary is not JSON, and one in the stream breaks every reader after it.
func TestOversizedBodyIsDroppedWholeAndFlagged(t *testing.T) {
	c := collecting(t)
	s := &Service{enabled: true, bodies: true, maxPayload: 32}
	msg := traceMessage(t, map[string]any{"blob": strings.Repeat("x", 512)})

	s.onBlockEvent(context.Background(), blockEvent(msg))
	record := c.only(t)

	if record.Body != nil {
		t.Errorf("Body = %s, want it omitted", record.Body)
	}
	if !record.Truncated {
		t.Error("an omitted body was not flagged as truncated")
	}
	if size, ok := record.Attrs["bodyBytes"].(int); !ok || size < 512 {
		t.Errorf("bodyBytes = %v, want the size that was refused", record.Attrs["bodyBytes"])
	}
}

func TestFlowListenerRecordsTheOutcome(t *testing.T) {
	c := collecting(t)
	s := capturingService()
	msg := traceMessage(t, nil)

	s.onFlowEvent(types.FlowEvent{
		Kind:       types.FlowEventFailed,
		Flow:       "orders",
		EventID:    msg.EventID,
		TraceID:    msg.TraceID(),
		OccurredAt: time.Now(),
		Duration:   7 * time.Millisecond,
		Err:        errors.New("upstream refused"),
		Block:      "rest/charge",
		Result:     msg,
	})

	record := c.only(t)
	if record.Kind != types.TraceFlowFailed {
		t.Errorf("Kind = %s, want %s", record.Kind, types.TraceFlowFailed)
	}
	if record.Err != "upstream refused" {
		t.Errorf("Err = %q", record.Err)
	}
	// The failing block is a label, not an address, so it must not land in Path
	// where everything else is addressable.
	if record.Path != "" {
		t.Errorf("Path = %q, want the failing block in Attrs instead", record.Path)
	}
	if got := record.Attrs["block"]; got != "rest/charge" {
		t.Errorf("Attrs[block] = %v", got)
	}
	if record.TraceID != msg.TraceID() {
		t.Errorf("TraceID = %q, want %q", record.TraceID, msg.TraceID())
	}
}

// The listeners stay registered for the life of the process — the block
// dispatcher has no unsubscribe — so being registered cannot be what decides
// whether a record is made. The tracer being enabled is.
func TestListenersAreInertWhileTheTracerIsDisabled(t *testing.T) {
	previous := core.Tracer()
	core.SetTracer(nil)
	t.Cleanup(func() { core.SetTracer(previous) })

	s := capturingService()
	msg := traceMessage(t, map[string]any{"amount": float64(12)})

	// Neither should panic, allocate a record, or touch the message.
	s.onBlockEvent(context.Background(), blockEvent(msg))
	s.onFlowEvent(types.FlowEvent{Kind: types.FlowEventCompleted, Flow: "orders", Result: msg})
}
