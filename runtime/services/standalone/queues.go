package standalone

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// queueBuffer is the per-subject channel capacity. Publish is non-blocking up to
// this many in-flight messages; beyond it Publish returns errQueueFull so a
// runaway producer surfaces backpressure instead of growing memory unbounded.
const queueBuffer = 1024

// errQueueFull is returned by Publish/Request when a subject's buffer is full.
var errQueueFull = errors.New("queues: subject buffer full")

// errNoResponder is returned by Request when no subscriber replied before the
// deadline (the in-process analogue of a request timing out with no responder).
var errNoResponder = errors.New("queues: no reply before deadline")

// envelope is one queued message plus its optional reply channel. A nil reply
// marks a fire-and-forget Publish; a non-nil reply is the in-process analogue of
// NATS's replyTo, and the subscriber sends the handler's reply down it.
type envelope struct {
	msg   types.Message
	reply chan types.Message
}

// queues is the standalone, in-process message queue: one buffered channel per
// subject, with competing consumers reading the same channel. A single process
// has nothing to coordinate, so there is no broker — the channel itself gives
// point-to-point delivery (each message is received by exactly one ranging
// goroutine) and the reply channel gives request-reply.
type queues struct {
	mu       sync.Mutex
	subjects map[string]chan envelope
}

func newQueues() *queues {
	return &queues{subjects: make(map[string]chan envelope)}
}

// channel returns the buffered channel for subject, creating it on first use so a
// subscriber that registers before any publisher (or vice versa) shares one
// channel.
func (q *queues) channel(subject string) chan envelope {
	q.mu.Lock()
	defer q.mu.Unlock()
	ch, ok := q.subjects[subject]
	if !ok {
		ch = make(chan envelope, queueBuffer)
		q.subjects[subject] = ch
	}
	return ch
}

// Publish enqueues msg for one competing consumer without waiting for a reply.
func (q *queues) Publish(ctx context.Context, subject string, msg types.Message) error {
	return q.enqueue(ctx, subject, envelope{msg: *msg.Clone()})
}

// Request enqueues msg with a reply channel and waits for the handler's reply,
// bounded by ctx and the configured timeout.
func (q *queues) Request(
	ctx context.Context, subject string, msg types.Message, opts ...core.RequestOption,
) (types.Message, error) {
	cfg := core.NewRequestConfig(opts...)
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	reply := make(chan types.Message, 1)
	if err := q.enqueue(ctx, subject, envelope{msg: *msg.Clone(), reply: reply}); err != nil {
		return types.Message{}, err
	}
	select {
	case r := <-reply:
		return r, nil
	case <-ctx.Done():
		return types.Message{}, errNoResponder
	}
}

// enqueue performs the non-blocking send shared by Publish and Request: it returns
// errQueueFull when the buffer is full and respects context cancellation.
func (q *queues) enqueue(ctx context.Context, subject string, env envelope) error {
	ch := q.channel(subject)
	select {
	case ch <- env:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("queues: enqueue %q: %w", subject, ctx.Err())
	default:
		return errQueueFull
	}
}

// Subscribe starts cfg.Listeners goroutines that compete for messages on subject.
// Each runs handler and, when the message carried a reply channel, forwards the
// handler's reply. Closing the returned subscription stops the goroutines.
//
//nolint:ireturn // satisfies core.Queues
func (q *queues) Subscribe(
	ctx context.Context, subject string, handler core.QueueHandler, opts ...core.SubscribeOption,
) (core.Subscription, error) {
	cfg := core.NewSubscribeConfig(opts...)
	ch := q.channel(subject)
	subCtx, cancel := context.WithCancel(ctx)

	var wg sync.WaitGroup
	wg.Add(cfg.Listeners)
	for i := 0; i < cfg.Listeners; i++ {
		go func() {
			defer wg.Done()
			consume(subCtx, ch, handler)
		}()
	}
	return &subscription{cancel: cancel, wg: &wg}, nil
}

// consume ranges the subject channel until the subscription is cancelled, running
// handler for each message and forwarding a reply when one was requested. A
// handler error on a fire-and-forget message is dropped (at-most-once); on a
// request the reply channel simply goes unfilled and the requester times out.
func consume(ctx context.Context, ch <-chan envelope, handler core.QueueHandler) {
	for {
		select {
		case <-ctx.Done():
			return
		case env := <-ch:
			reply, err := handler(ctx, env.msg)
			if env.reply == nil || err != nil {
				continue
			}
			select {
			case env.reply <- reply:
			default:
			}
		}
	}
}

// subscription cancels its goroutines and waits for them to drain on Close, so a
// caller that closes a subscription knows no handler is still running afterwards.
type subscription struct {
	cancel context.CancelFunc
	wg     *sync.WaitGroup
	once   sync.Once
}

func (s *subscription) Close() error {
	s.once.Do(func() {
		s.cancel()
		s.wg.Wait()
	})
	return nil
}
