package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

func newTopicFixture(t *testing.T) (*Services, *topicBackend) {
	t.Helper()
	f := newFake(t, fastPoll())
	b := newTopicBackend()
	b.install(f)
	return newTestServices(t, f, nil), b
}

// topicCollector gathers what a topic subscription handled.
type topicCollector struct {
	mu  sync.Mutex
	got []types.Message
	ch  chan struct{}
}

func newTopicCollector() *topicCollector {
	return &topicCollector{ch: make(chan struct{}, 64)}
}

func (c *topicCollector) handle(_ context.Context, msg types.Message) error {
	c.mu.Lock()
	c.got = append(c.got, msg)
	c.mu.Unlock()
	c.ch <- struct{}{}
	return nil
}

func (c *topicCollector) await(t *testing.T, n int, why string) {
	t.Helper()
	for range n {
		select {
		case <-c.ch:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d handled: %s", len(c.got), n, why)
		}
	}
}

// Fan-out is the plane's whole job: every subscriber receives every message,
// which is what the per-subscription cursor exists to make possible over a pull
// API.
func TestTopicBroadcastsToEverySubscriber(t *testing.T) {
	svc, _ := newTopicFixture(t)
	first, second := newTopicCollector(), newTopicCollector()

	subA, err := svc.Topics().Subscribe(t.Context(), "events", first.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = subA.Close() }()
	subB, err := svc.Topics().Subscribe(t.Context(), "events", second.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = subB.Close() }()

	msg := newTestMessage(t, "")
	msg.Body = map[string]any{"kind": "deployed"}
	if err := svc.Topics().Publish(t.Context(), "events", *msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	first.await(t, 1, "the first subscriber did not receive the broadcast")
	second.await(t, 1, "the second subscriber did not receive the broadcast — "+
		"a topic is not a competing-consumer queue")
}

// A system: subject publishes unscoped, with a flag saying so, so an implementer
// routes it deliberately rather than by parsing the subject name.
func TestSystemSubjectPublishesUnscopedWithAFlag(t *testing.T) {
	svc, b := newTopicFixture(t)

	msg := newTestMessage(t, "")
	if err := svc.Topics().Publish(t.Context(), systemPrefix+"internal.logs", *msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.systemPublishes) != 1 || b.systemPublishes[0] != "internal.logs" {
		t.Fatalf("system publishes = %v, want the prefix stripped and the flag set", b.systemPublishes)
	}
}

// Subscribing to a system: subject is refused. The prefix exists so a flow can
// raise a platform event; subscribing would let it read traffic belonging to
// workloads it has nothing to do with.
func TestSystemSubjectCannotBeSubscribedTo(t *testing.T) {
	svc, _ := newTopicFixture(t)
	c := newTopicCollector()

	_, err := svc.Topics().Subscribe(t.Context(), systemPrefix+"internal.logs", c.handle)
	if err == nil {
		t.Fatal("Subscribe err = nil, want a refusal")
	}
	if !strings.Contains(err.Error(), systemPrefix) {
		t.Fatalf("err = %v, want it to name the prefix", err)
	}
}

// A subscription left behind accumulates messages nobody will read, so Close
// unregisters it.
func TestTopicCloseRemovesTheSubscription(t *testing.T) {
	svc, b := newTopicFixture(t)
	c := newTopicCollector()

	sub, err := svc.Topics().Subscribe(t.Context(), "events", c.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if got := b.subscriptions(); got != 1 {
		t.Fatalf("subscriptions = %d, want 1", got)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := b.subscriptions(); got != 0 {
		t.Fatalf("subscriptions = %d after Close, want 0: a durable cursor was left behind", got)
	}
}

// Without a subscription id there is no cursor to poll against, so the failure
// says that rather than starting a loop that 404s forever.
func TestTopicSubscribeNeedsASubscriptionID(t *testing.T) {
	f := newFake(t, fastPoll())
	f.mux.HandleFunc("POST "+pathTopicSubs, func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, subscribeResponse{})
	})
	svc := newTestServices(t, f, nil)
	c := newTopicCollector()

	_, err := svc.Topics().Subscribe(t.Context(), "events", c.handle)
	if err == nil || !strings.Contains(err.Error(), "subscriptionId") {
		t.Fatalf("Subscribe = %v, want a failure naming the missing subscriptionId", err)
	}
}

// A handler error is logged and dropped, not redelivered: core.TopicHandler says
// a topic has no requester to surface one to, so holding the message back would
// contradict the interface.
func TestTopicHandlerErrorDoesNotRedeliver(t *testing.T) {
	svc, _ := newTopicFixture(t)
	var calls int
	var mu sync.Mutex
	done := make(chan struct{}, 4)

	handler := func(context.Context, types.Message) error {
		mu.Lock()
		calls++
		mu.Unlock()
		done <- struct{}{}
		return errFakeHandler
	}
	sub, err := svc.Topics().Subscribe(t.Context(), "events", handler)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	msg := newTestMessage(t, "")
	if err := svc.Topics().Publish(t.Context(), "events", *msg); err != nil {
		t.Fatal(err)
	}
	<-done
	select {
	case <-done:
		t.Fatal("the message was redelivered after the handler failed")
	case <-time.After(2 * time.Second):
	}
}

// errFakeHandler is what a deliberately failing handler returns.
var errFakeHandler = errors.New("handler failed on purpose")
