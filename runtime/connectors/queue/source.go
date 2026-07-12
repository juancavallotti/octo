package queue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// errSourceStopped is returned by a handler that is interrupted because the source
// is shutting down before the flow produced a terminal outcome.
var errSourceStopped = errors.New("queue source: stopped")

// sourceSettings configures one Platform Queue subscription bound to a flow.
type sourceSettings struct {
	// Queue subject to subscribe to. Every replica subscribed to the same subject
	// competes for messages, so each message is handled once across the cluster.
	Subject string `json:"subject" octo:"label=Subject,required"`
	// Number of concurrent handler goroutines; defaults to the queue service default.
	Listeners int `json:"listeners" octo:"label=Listeners"`
	// How long a handler waits for its flow to finish (e.g. 30s); defaults to the
	// queue service timeout.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string"`
}

// source subscribes to a queue subject and turns each delivered message into a
// flow execution. For a message that came from a Request it returns the flow's
// result as the reply (correlated through the connector's pending registry on the
// flow-event bus); for a fire-and-forget Publish the queue layer simply drops the
// reply. Either way the handler holds a listener until the flow finishes, which
// bounds in-flight work to the listener count.
type source struct {
	conn      *Connector
	out       chan<- *types.Message
	subject   string
	listeners int
	timeout   time.Duration

	sub      core.Subscription
	done     chan struct{}
	stopOnce sync.Once
}

// NewSource builds a queue source, validating its subject up front. The
// subscription itself is opened in Start, where the runtime services (and so the
// queue backend) are available on the context.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *Connector) NewSource(
	cfg types.SourceConfig, out chan<- *types.Message, _ core.SourceDeps,
) (core.MessageSource, error) {
	var set sourceSettings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	if strings.TrimSpace(set.Subject) == "" {
		return nil, errors.New("queue source requires a \"subject\" setting")
	}
	return &source{
		conn:      c,
		out:       out,
		subject:   set.Subject,
		listeners: set.Listeners,
		timeout:   time.Duration(set.Timeout),
		done:      make(chan struct{}),
	}, nil
}

// Start subscribes to the subject on the core queue service. The handler runs on
// the queue's listener goroutines; Subscribe does not block.
func (s *source) Start(ctx context.Context) error {
	q := core.RuntimeServicesFromContext(ctx).Queues()
	var opts []core.SubscribeOption
	if s.listeners > 0 {
		opts = append(opts, core.WithListeners(s.listeners))
	}
	sub, err := q.Subscribe(ctx, s.subject, s.handle, opts...)
	if err != nil {
		return fmt.Errorf("queue source: subscribe to %q: %w", s.subject, err)
	}
	s.sub = sub
	slog.Info("queue subscription active", "subject", s.subject)
	return nil
}

// Stop closes the subscription, which cancels the handler context and waits for
// in-flight handlers to drain, so the runtime can safely close the output channel
// afterwards. Closing done first unblocks a handler parked on a full output
// channel or awaiting its flow.
func (s *source) Stop(context.Context) error {
	s.stopOnce.Do(func() { close(s.done) })
	if s.sub != nil {
		return s.sub.Close()
	}
	return nil
}

// handle runs one queued message through the flow and returns the flow's result
// as the reply. It tracks the message's EventID before sending so the terminal
// flow event can be matched back; a fire-and-forget sender's reply is discarded by
// the queue layer, so the same handler serves both send shapes.
func (s *source) handle(ctx context.Context, in types.Message) (types.Message, error) {
	ch := s.conn.track(in.EventID)
	defer s.conn.forget(in.EventID)

	msg := in
	if !s.send(ctx, &msg) {
		return types.Message{}, errSourceStopped
	}
	return s.awaitResult(ctx, ch)
}

// send delivers msg onto the flow channel, aborting if the message context or the
// source is shutting down.
func (s *source) send(ctx context.Context, msg *types.Message) bool {
	select {
	case s.out <- msg:
		return true
	case <-ctx.Done():
		return false
	case <-s.done:
		return false
	}
}

// awaitResult blocks until the flow finishes, the wait times out, the context is
// cancelled, or the source stops, and maps the outcome to a reply. A completed or
// dropped flow returns its message (dropped's is the unchanged input); a failed
// flow returns the error, which a Request surfaces to the caller.
func (s *source) awaitResult(ctx context.Context, ch chan result) (types.Message, error) {
	timeout := s.timeout
	if timeout <= 0 {
		timeout = core.DefaultRequestTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case res := <-ch:
		switch res.kind {
		case types.FlowEventCompleted, types.FlowEventDropped:
			if res.msg != nil {
				return *res.msg, nil
			}
			return types.Message{}, nil
		case types.FlowEventFailed:
			if res.err != nil {
				return types.Message{}, res.err
			}
			return types.Message{}, fmt.Errorf("queue source %q: flow failed", s.subject)
		default:
			return types.Message{}, fmt.Errorf("queue source %q: unexpected flow outcome %q", s.subject, res.kind)
		}
	case <-timer.C:
		return types.Message{}, fmt.Errorf("queue source %q: flow timed out", s.subject)
	case <-ctx.Done():
		return types.Message{}, fmt.Errorf("queue source %q: %w", s.subject, ctx.Err())
	case <-s.done:
		return types.Message{}, errSourceStopped
	}
}
