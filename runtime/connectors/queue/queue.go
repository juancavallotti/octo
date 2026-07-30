// Package queue exposes the platform's core queue service to flows as first-class
// DSL constructs: a message source that runs each queued message through a flow,
// and the "queue-dispatch" block that sends the current message to a subject.
// Together they make the cluster's competing-consumer queues the way flows load
// balance work across replicas — the cross-replica analogue of the in-process
// flow-ref block.
//
// The queue itself is a core runtime service (core.Queues, reached via
// core.RuntimeServicesFromContext), in-process in the standalone module and
// NATS-backed in the k8s module. This connector holds no transport of its own; it
// only owns the request/response correlation needed to turn a queue Request into a
// flow execution and return the flow's result as the reply. That correlation rides
// the process-wide flow-event bus exactly as the HTTP connector does: every
// terminal FlowEvent carries the result message keyed by EventID, which a parked
// source handler matches against its pending registry.
package queue

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// init is this module's manifest: the one place that says what importing this
// package puts into the runtime, in a deterministic order. Each block's own
// registration lives beside the block as a registerX function called from here.
func init() {
	registerConnector()
	registerDispatch()
}

func registerConnector() {
	core.MustRegisterConnector("queue", func() core.Connector {
		return &Connector{}
	})

	// The queue connector carries no settings — it is source-only. Its Platform
	// Queue source subscribes to the core queue service directly.
	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:  "queue",
		Label: "Platform Queue",
		Icon:  "Inbox",
		Sources: []core.SourceMeta{{
			Type:     "queue",
			Label:    "Platform Queue",
			Icon:     "Inbox",
			Settings: reflect.TypeFor[sourceSettings](),
		}},
	})
}

// result is the outcome the event-bus handler delivers to a parked source handler.
type result struct {
	kind types.FlowEventKind
	msg  *types.Message
	err  error
}

// Connector owns the request/response registry shared by the queue sources it
// builds. It has no settings and no transport: a source subscribes to the core
// queue service itself (from the context at start), and the connector only
// rendezvouses completed flows back to the parked handlers through its pending
// map, fed by the flow-event bus it subscribes to once in Start.
type Connector struct {
	done        chan struct{}
	unsubscribe func()

	mu      sync.Mutex
	pending map[string]chan result
}

// Start initializes the pending registry and subscribes once to the flow-event
// bus so terminal events can be matched back to parked source handlers. It needs
// no settings.
func (c *Connector) Start(context.Context, types.ConnectorConfig) error {
	c.done = make(chan struct{})
	c.pending = make(map[string]chan result)
	c.unsubscribe = core.DefaultEventBus().Subscribe(c.onFlowEvent)
	return nil
}

// Stop unblocks any parked source handlers and stops correlating flow events.
func (c *Connector) Stop(context.Context) error {
	if c.done != nil {
		close(c.done)
	}
	if c.unsubscribe != nil {
		c.unsubscribe()
	}
	return nil
}

// track registers a buffered reply channel under eventID and returns it. The
// buffer of one lets onFlowEvent deliver without ever blocking the flow worker.
func (c *Connector) track(eventID string) chan result {
	ch := make(chan result, 1)
	c.mu.Lock()
	c.pending[eventID] = ch
	c.mu.Unlock()
	return ch
}

// forget removes the pending entry for eventID; safe to call more than once.
func (c *Connector) forget(eventID string) {
	c.mu.Lock()
	delete(c.pending, eventID)
	c.mu.Unlock()
}

// onFlowEvent delivers a terminal flow event to the matching parked handler. It
// runs synchronously on the flow worker, so it never blocks: the reply channel is
// buffered and the send is non-blocking. Started events carry no result and are
// ignored.
func (c *Connector) onFlowEvent(ev types.FlowEvent) {
	if ev.Kind == types.FlowEventStarted {
		return
	}
	c.mu.Lock()
	ch, ok := c.pending[ev.EventID]
	c.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- result{kind: ev.Kind, msg: ev.Result, err: ev.Err}:
	default:
	}
}

// duration decodes either a Go duration string ("5s") or a numeric nanosecond
// count from settings, since settings round-trip through JSON.
type duration time.Duration

// UnmarshalJSON parses a duration from a quoted string ("250ms") or a number.
func (d *duration) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		parsed, err := time.ParseDuration(strings.Trim(s, `"`))
		if err != nil {
			return fmt.Errorf("parse duration: %w", err)
		}
		*d = duration(parsed)
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("parse duration: %w", err)
	}
	*d = duration(n)
	return nil
}
