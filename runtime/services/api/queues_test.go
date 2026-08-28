package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fastPoll shortens the long poll so a test does not wait twenty seconds for an
// empty one.
func fastPoll() discoveryDocument {
	doc := fullDiscovery()
	doc.Features.Queues.PollTimeoutSeconds = 1
	doc.Features.Topics.PollTimeoutSeconds = 1
	return doc
}

func newQueueFixture(t *testing.T) (*Services, *queueBackend) {
	t.Helper()
	f := newFake(t, fastPoll())
	b := newQueueBackend()
	b.install(f)
	return newTestServices(t, f, nil), b
}

// collector gathers what a subscription handled.
type collector struct {
	mu   sync.Mutex
	got  []types.Message
	fail func(types.Message) error
	ch   chan struct{}
}

func newCollector() *collector { return &collector{ch: make(chan struct{}, 64)} }

func (c *collector) handle(_ context.Context, msg types.Message) (types.Message, error) {
	c.mu.Lock()
	c.got = append(c.got, msg)
	fail := c.fail
	c.mu.Unlock()
	c.ch <- struct{}{}
	if fail != nil {
		if err := fail(msg); err != nil {
			return types.Message{}, err
		}
	}
	reply, err := types.NewMessage("")
	if err != nil {
		return types.Message{}, err
	}
	reply.Body = map[string]any{"ok": true}
	return *reply, nil
}

// await waits for n handler invocations.
func (c *collector) await(t *testing.T, n int, why string) {
	t.Helper()
	for range n {
		select {
		case <-c.ch:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d handled: %s", len(c.got), n, why)
		}
	}
}

// A published message reaches a subscriber whole, which is the plane's whole job.
func TestQueuePublishReachesASubscriber(t *testing.T) {
	svc, _ := newQueueFixture(t)
	c := newCollector()

	sub, err := svc.Queues().Subscribe(t.Context(), "orders", c.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	msg := newTestMessage(t, "corr-9")
	msg.Body = map[string]any{"id": "A-1"}
	if err := svc.Queues().Publish(t.Context(), "orders", *msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	c.await(t, 1, "the published message never arrived")
	got := c.got[0]
	if got.CorrelationID != "corr-9" {
		t.Errorf("correlationId = %q, want corr-9", got.CorrelationID)
	}
	body, ok := got.Body.(map[string]any)
	if !ok || body["id"] != "A-1" {
		t.Errorf("body = %#v", got.Body)
	}
}

// Delivery here is at-least-once: a handler that failed nacks, and the platform
// hands the message back. That is the difference from the other two modules.
func TestQueueNacksAndRedeliversOnHandlerFailure(t *testing.T) {
	svc, b := newQueueFixture(t)
	c := newCollector()

	var failures int
	var mu sync.Mutex
	c.fail = func(types.Message) error {
		mu.Lock()
		defer mu.Unlock()
		failures++
		if failures == 1 {
			return errors.New("transient")
		}
		return nil
	}

	sub, err := svc.Queues().Subscribe(t.Context(), "orders", c.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	msg := newTestMessage(t, "")
	if err := svc.Queues().Publish(t.Context(), "orders", *msg); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	c.await(t, 2, "the failed message was not redelivered")
	if b.attempts("orders") < 2 {
		t.Fatalf("attempts = %d, want at least 2", b.attempts("orders"))
	}
}

// A handled message is acknowledged, so it is not redelivered forever.
func TestQueueAcksAHandledMessage(t *testing.T) {
	svc, _ := newQueueFixture(t)
	c := newCollector()
	sub, err := svc.Queues().Subscribe(t.Context(), "orders", c.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	msg := newTestMessage(t, "")
	if err := svc.Queues().Publish(t.Context(), "orders", *msg); err != nil {
		t.Fatal(err)
	}
	c.await(t, 1, "the message never arrived")

	// Give the ack a moment, then confirm nothing comes back.
	select {
	case <-c.ch:
		t.Fatal("the message was redelivered after being handled")
	case <-time.After(2 * time.Second):
	}
}

// Request-reply: the handler's answer reaches the requester.
func TestQueueRequestReply(t *testing.T) {
	svc, _ := newQueueFixture(t)
	c := newCollector()

	sub, err := svc.Queues().Subscribe(t.Context(), "pricing", c.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	msg := newTestMessage(t, "")
	msg.Body = map[string]any{"sku": "X"}
	reply, err := svc.Queues().Request(t.Context(), "pricing", *msg, core.WithTimeout(10*time.Second))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	body, ok := reply.Body.(map[string]any)
	if !ok || body["ok"] != true {
		t.Fatalf("reply body = %#v", reply.Body)
	}
}

// A platform that implements queues but not request-reply says so in discovery,
// and the refusal names what to implement rather than timing out.
func TestQueueRequestRefusedWithoutRequestReply(t *testing.T) {
	doc := fastPoll()
	doc.Features.Queues.RequestReply = false
	f := newFake(t, doc)
	newQueueBackend().install(f)
	svc := newTestServices(t, f, nil)

	msg := newTestMessage(t, "")
	_, err := svc.Queues().Request(t.Context(), "pricing", *msg)
	if err == nil || !strings.Contains(err.Error(), "requestReply") {
		t.Fatalf("Request = %v, want a refusal naming the discovery flag", err)
	}
}

// An empty poll is a 204, not an error, and the loop simply asks again. This is
// the single most common thing an implementer gets wrong.
func TestEmptyPollIsNotAnError(t *testing.T) {
	svc, b := newQueueFixture(t)
	b.mu.Lock()
	b.pollDelay = 50 * time.Millisecond
	b.mu.Unlock()
	c := newCollector()

	sub, err := svc.Queues().Subscribe(t.Context(), "idle", c.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	// Let several polls expire, then publish: a loop that treated 204 as an error
	// would have backed off and would not pick this up promptly.
	time.Sleep(300 * time.Millisecond)
	msg := newTestMessage(t, "")
	if err := svc.Queues().Publish(t.Context(), "idle", *msg); err != nil {
		t.Fatal(err)
	}
	c.await(t, 1, "the loop stopped polling after an empty poll")
}

// Close stops delivery and drains the workers, so no handler is still running
// when it returns.
func TestQueueSubscriptionCloseDrains(t *testing.T) {
	svc, _ := newQueueFixture(t)
	c := newCollector()
	sub, err := svc.Queues().Subscribe(t.Context(), "orders", c.handle)
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A second Close must not panic on an already-stopped loop.
	if err := sub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// One poll goroutine per subscription, not one per listener: the platform should
// see a single long-poll connection however many workers run behind it.
func TestOnePollPerSubscription(t *testing.T) {
	f := newFake(t, fastPoll())
	newQueueBackend().install(f)
	svc := newTestServices(t, f, nil)
	c := newCollector()

	sub, err := svc.Queues().Subscribe(t.Context(), "orders", c.handle, core.WithListeners(8))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	// One poll timeout is one second, so in well under two the loop cannot have
	// made more than a couple of sequential polls — but eight parallel ones would
	// show up immediately.
	time.Sleep(200 * time.Millisecond)
	if got := f.count(http.MethodPost, "/receive"); got > 1 {
		t.Fatalf("receive calls = %d in one poll window, want 1: the loop is polling per listener", got)
	}
}

// A subject may contain a slash, which must reach the server as one segment
// rather than inventing a path.
func TestQueueSubjectIsEscaped(t *testing.T) {
	f := newFake(t, fastPoll())
	newQueueBackend().install(f)
	svc := newTestServices(t, f, nil)

	msg := newTestMessage(t, "")
	// The fake routes on {subject}, so a slash that leaked would 404 rather than
	// reach the handler.
	if err := svc.Queues().Publish(t.Context(), "orders/new", *msg); err != nil {
		t.Fatalf("Publish with a slashed subject: %v", err)
	}
	req := f.last("/publish")
	if !strings.Contains(req.path, "orders%2Fnew") && !strings.Contains(req.path, "orders/new") {
		t.Fatalf("path = %s, want the subject as one segment", req.path)
	}
	var body publishRequest
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatalf("the publish body is not the documented envelope: %v", err)
	}
	if body.Message.CorrelationID != msg.CorrelationID {
		t.Fatalf("envelope correlationId = %q", body.Message.CorrelationID)
	}
}
