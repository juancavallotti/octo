package standalone

import (
	"context"
	"log/slog"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// topicBuffer is the per-subscription channel capacity. Publish is non-blocking:
// once a subscriber's buffer is full its further messages are dropped (at-most-once
// fan-out), so one slow subscriber cannot stall the publisher or the others.
const topicBuffer = 1024

// topics is the standalone, in-process broadcast pub/sub: every subscription on a
// subject gets its own buffered channel, and Publish fans a copy of the message out
// to all of them. A single process has no broker — the per-subscription channels
// give the fan-out that NATS plain subscriptions give in the cluster module.
type topics struct {
	mu   sync.Mutex
	subs map[string]map[*topicSubscription]struct{}
}

func newTopics() *topics {
	return &topics{subs: make(map[string]map[*topicSubscription]struct{})}
}

// Publish delivers a copy of msg to every current subscriber on subject. A
// subscriber whose buffer is full is skipped rather than blocking the publisher.
func (t *topics) Publish(_ context.Context, subject string, msg types.Message) error {
	t.mu.Lock()
	subs := make([]*topicSubscription, 0, len(t.subs[subject]))
	for sub := range t.subs[subject] {
		subs = append(subs, sub)
	}
	t.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub.ch <- *msg.Clone():
		default: // buffer full: drop for this subscriber (at-most-once)
		}
	}
	return nil
}

// Subscribe registers a fan-out subscriber on subject and starts cfg.Listeners
// goroutines that run handler for each delivered message. Closing the returned
// subscription unregisters it and stops the goroutines.
//
//nolint:ireturn // satisfies core.Topics
func (t *topics) Subscribe(
	ctx context.Context, subject string, handler core.TopicHandler, opts ...core.SubscribeOption,
) (core.Subscription, error) {
	cfg := core.NewSubscribeConfig(opts...)
	subCtx, cancel := context.WithCancel(ctx)
	sub := &topicSubscription{
		parent:  t,
		subject: subject,
		ch:      make(chan types.Message, topicBuffer),
		cancel:  cancel,
		wg:      &sync.WaitGroup{},
	}

	t.mu.Lock()
	if t.subs[subject] == nil {
		t.subs[subject] = make(map[*topicSubscription]struct{})
	}
	t.subs[subject][sub] = struct{}{}
	t.mu.Unlock()

	sub.wg.Add(cfg.Listeners)
	for i := 0; i < cfg.Listeners; i++ {
		go func() {
			defer sub.wg.Done()
			consumeTopic(subCtx, subject, sub.ch, handler)
		}()
	}
	return sub, nil
}

// consumeTopic runs handler for each delivered message until the subscription is
// cancelled, logging (and otherwise ignoring) a handler error since a topic has no
// requester to surface it to.
func consumeTopic(ctx context.Context, subject string, ch <-chan types.Message, handler core.TopicHandler) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			if err := handler(ctx, msg); err != nil {
				slog.Error("topics: handler", "subject", subject, "err", err)
			}
		}
	}
}

// topicSubscription is one fan-out subscriber. Close removes it from its parent so
// no further messages are enqueued, then cancels and drains its goroutines.
type topicSubscription struct {
	parent  *topics
	subject string
	ch      chan types.Message
	cancel  context.CancelFunc
	wg      *sync.WaitGroup
	once    sync.Once
}

func (s *topicSubscription) Close() error {
	s.once.Do(func() {
		s.parent.mu.Lock()
		if subs := s.parent.subs[s.subject]; subs != nil {
			delete(subs, s)
			if len(subs) == 0 {
				delete(s.parent.subs, s.subject)
			}
		}
		s.parent.mu.Unlock()
		s.cancel()
		s.wg.Wait()
	})
	return nil
}
