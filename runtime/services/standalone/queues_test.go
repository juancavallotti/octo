package standalone

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// echoBody returns a message whose body is the string s.
func bodyMessage(s string) types.Message {
	return types.Message{Body: s}
}

func TestQueuesRequestReply(t *testing.T) {
	q := newQueues()
	ctx := context.Background()

	sub, err := q.Subscribe(ctx, "greet", func(_ context.Context, m types.Message) (types.Message, error) {
		name, _ := m.Body.(string)
		return bodyMessage("hello " + name), nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	reply, err := q.Request(ctx, "greet", bodyMessage("world"))
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if got, _ := reply.Body.(string); got != "hello world" {
		t.Fatalf("reply body = %q, want %q", got, "hello world")
	}
}

// TestQueuesRequestPropagatesVars verifies vars survive the round-trip in both
// directions through the in-process queue.
func TestQueuesRequestPropagatesVars(t *testing.T) {
	q := newQueues()
	ctx := context.Background()

	sub, err := q.Subscribe(ctx, "vars", func(_ context.Context, m types.Message) (types.Message, error) {
		tenant, ok := m.Variables.String("tenant")
		if !ok {
			t.Errorf("inbound missing tenant var")
		}
		out := bodyMessage("ok")
		out.Variables.Set("seen", tenant)
		return out, nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	in := bodyMessage("req")
	in.Variables.Set("tenant", "acme")

	reply, err := q.Request(ctx, "vars", in)
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if got, _ := reply.Variables.String("seen"); got != "acme" {
		t.Fatalf("reply var seen = %q, want %q", got, "acme")
	}
}

// TestQueuesPublishFireAndForget verifies Publish delivers to a consumer and does
// not require (or wait for) a reply.
func TestQueuesPublishFireAndForget(t *testing.T) {
	q := newQueues()
	ctx := context.Background()

	got := make(chan string, 1)
	sub, err := q.Subscribe(ctx, "work", func(_ context.Context, m types.Message) (types.Message, error) {
		got <- m.Body.(string)
		// Returning a reply with no requester must be harmless (dropped).
		return bodyMessage("ignored"), nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	if err := q.Publish(ctx, "work", bodyMessage("task")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case s := <-got:
		if s != "task" {
			t.Fatalf("delivered %q, want %q", s, "task")
		}
	case <-time.After(time.Second):
		t.Fatal("message not delivered")
	}
}

// TestQueuesCompetingConsumers verifies each published message is handled exactly
// once across N concurrent listeners.
func TestQueuesCompetingConsumers(t *testing.T) {
	q := newQueues()
	ctx := context.Background()

	const total = 200
	var handled atomic.Int64
	var wg sync.WaitGroup
	wg.Add(total)

	sub, err := q.Subscribe(ctx, "jobs", func(_ context.Context, _ types.Message) (types.Message, error) {
		handled.Add(1)
		wg.Done()
		return types.Message{}, nil
	}, core.WithListeners(4))
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	for i := 0; i < total; i++ {
		if err := q.Publish(ctx, "jobs", types.Message{}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("only %d/%d handled", handled.Load(), total)
	}
	if got := handled.Load(); got != total {
		t.Fatalf("handled %d, want %d", got, total)
	}
}

// TestQueuesRequestTimeout verifies a Request to a subject with no responder fails
// once the timeout elapses rather than blocking forever.
func TestQueuesRequestTimeout(t *testing.T) {
	q := newQueues()
	ctx := context.Background()

	start := time.Now()
	_, err := q.Request(ctx, "void", bodyMessage("hi"), core.WithTimeout(50*time.Millisecond))
	if !errors.Is(err, errNoResponder) {
		t.Fatalf("err = %v, want errNoResponder", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Request blocked too long: %v", elapsed)
	}
}

// TestQueuesPublishBackpressure verifies Publish reports a full buffer instead of
// blocking when no consumer drains the subject.
func TestQueuesPublishBackpressure(t *testing.T) {
	q := newQueues()
	ctx := context.Background()

	// Fill the buffer exactly, then the next publish must report it as full.
	for i := 0; i < queueBuffer; i++ {
		if err := q.Publish(ctx, "full", types.Message{}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}
	if err := q.Publish(ctx, "full", types.Message{}); !errors.Is(err, errQueueFull) {
		t.Fatalf("err = %v, want errQueueFull", err)
	}
}

// TestQueuesSubscriptionClose verifies that after Close no further messages are
// delivered to the handler.
func TestQueuesSubscriptionClose(t *testing.T) {
	q := newQueues()
	ctx := context.Background()

	var handled atomic.Int64
	sub, err := q.Subscribe(ctx, "stop", func(_ context.Context, _ types.Message) (types.Message, error) {
		handled.Add(1)
		return types.Message{}, nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Publishing after Close buffers but is never consumed.
	if err := q.Publish(ctx, "stop", types.Message{}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if got := handled.Load(); got != 0 {
		t.Fatalf("handled %d after close, want 0", got)
	}
}
