// This file holds what the imap and pop3 sources share: a leader-gated poll
// loop, and handing one received message to the flow and waiting for its
// outcome. A message is only acted on once its flow completes, so the outcome
// is the whole point: a source that did not wait could only mark mail handled
// before anything had handled it.
package email

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Source defaults.
const (
	defaultInterval    = time.Minute
	defaultMaxPerPoll  = 10
	defaultFlowTimeout = 5 * time.Minute
)

// leaderKey names a source's leader-election key: the connector kind and the
// configured instance name, so every replica agrees on it and distinct mail
// connectors elect independently.
func leaderKey(kind string, cfg types.SourceConfig) string {
	name := cfg.Connector
	if name == "" {
		name = cfg.Type
	}
	return "email_" + kind + "_" + name
}

// pendingFlows correlates terminal flow events back to the source waiting on
// them, by EventID. Each mail connector owns one, subscribed to the flow-event
// bus for as long as the connector runs.
type pendingFlows struct {
	mu          sync.Mutex
	waiting     map[string]chan types.FlowEvent
	done        chan struct{}
	unsubscribe func()
	closeOnce   sync.Once
}

func newPendingFlows() *pendingFlows {
	p := &pendingFlows{waiting: map[string]chan types.FlowEvent{}, done: make(chan struct{})}
	p.unsubscribe = core.DefaultEventBus().Subscribe(p.onFlowEvent)
	return p
}

// track registers interest in eventID's outcome. The channel's buffer of one
// lets onFlowEvent deliver without ever blocking the flow worker.
func (p *pendingFlows) track(eventID string) chan types.FlowEvent {
	ch := make(chan types.FlowEvent, 1)
	p.mu.Lock()
	p.waiting[eventID] = ch
	p.mu.Unlock()
	return ch
}

func (p *pendingFlows) forget(eventID string) {
	p.mu.Lock()
	delete(p.waiting, eventID)
	p.mu.Unlock()
}

// onFlowEvent runs on the flow worker, so it never blocks.
func (p *pendingFlows) onFlowEvent(ev types.FlowEvent) {
	if ev.Kind == types.FlowEventStarted {
		return
	}
	p.mu.Lock()
	ch, ok := p.waiting[ev.EventID]
	p.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- ev:
	default:
	}
}

func (p *pendingFlows) close() {
	p.closeOnce.Do(func() {
		p.unsubscribe()
		close(p.done)
	})
}

// errStopped reports that the source stopped before the flow finished.
var errStopped = errors.New("source stopped")

// emitter hands messages to a flow and waits for each outcome.
type emitter struct {
	out         chan<- *types.Message
	pending     *pendingFlows
	flowTimeout time.Duration
}

// emit sends body to the flow and reports whether the flow handled it: true for
// a completed flow, and for a dropped one, since a flow that filters a message
// out has decided about it as surely as one that processed it. A failed flow or
// one that outlives flowTimeout is not handled — the message stays on the
// server and is read again on the next poll.
func (e emitter) emit(ctx context.Context, body map[string]any, attrs map[string]any) (bool, error) {
	msg, err := types.NewMessage("")
	if err != nil {
		return false, fmt.Errorf("build message: %w", err)
	}
	msg.SetBody(body)
	e.trace(msg, attrs)

	ch := e.pending.track(msg.EventID)
	defer e.pending.forget(msg.EventID)

	select {
	case e.out <- msg:
	case <-ctx.Done():
		return false, errStopped
	}

	timer := time.NewTimer(e.flowTimeout)
	defer timer.Stop()
	select {
	case ev := <-ch:
		switch ev.Kind {
		case types.FlowEventCompleted, types.FlowEventDropped:
			return true, nil
		default:
			return false, nil
		}
	case <-timer.C:
		slog.Warn("email source: flow outlived its timeout; the message will be read again",
			"timeout", e.flowTimeout, "eventId", msg.EventID)
		return false, nil
	case <-ctx.Done():
		return false, errStopped
	case <-e.pending.done:
		return false, errStopped
	}
}

// trace records where the message came from, minting the trace id so whatever
// the flow fans out to stays one trace.
func (e emitter) trace(msg *types.Message, attrs map[string]any) {
	tracer := core.Tracer()
	if !tracer.Enabled() {
		return
	}
	if _, err := msg.EnsureTraceID(); err != nil {
		slog.Warn("email source: could not generate a trace id", "error", err)
		return
	}
	tracer.Publish(types.TraceEvent{
		Kind:          types.TraceSourceEmit,
		TraceID:       msg.TraceID(),
		EventID:       msg.EventID,
		CorrelationID: msg.CorrelationID,
		OccurredAt:    time.Now(),
		Attrs:         attrs,
	})
}

// poller runs poll once at start and then every interval, while this replica
// holds the source's leadership. Polls run on the loop goroutine, so a poll that
// outlasts the interval delays the next one instead of overlapping it.
type poller struct {
	label     string
	leaderKey string
	interval  time.Duration
	poll      func(ctx context.Context) error

	lease  core.Leadership
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// start acquires leadership and starts the loop without blocking.
func (p *poller) start(ctx context.Context) error {
	lease, err := core.RuntimeServicesFromContext(ctx).LeaderElection().Acquire(ctx, p.leaderKey)
	if err != nil {
		return fmt.Errorf("%s source: acquire leadership for %q: %w", p.label, p.leaderKey, err)
	}
	p.lease = lease
	loopCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.wg.Add(1)
	go p.loop(loopCtx)
	return nil
}

func (p *poller) loop(ctx context.Context) {
	defer p.wg.Done()
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		p.tick(ctx)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

// tick polls once if this replica leads. A failed poll is logged and retried at
// the next interval: a mail server that is down for a minute is routine.
func (p *poller) tick(ctx context.Context) {
	if !p.lease.IsLeader() {
		slog.Debug("email poll skipped: not leader", "key", p.leaderKey)
		return
	}
	if err := p.poll(ctx); err != nil && !errors.Is(err, errStopped) && ctx.Err() == nil {
		slog.Error("email poll failed", "source", p.label, "key", p.leaderKey, "error", err)
	}
}

// stop ends the loop, waits for an in-flight poll, and releases leadership, so
// nothing is sent after it returns.
func (p *poller) stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	if p.lease != nil {
		_ = p.lease.Close()
	}
}
