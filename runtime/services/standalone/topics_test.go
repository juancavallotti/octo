package standalone

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// TestTopicsFanOut verifies every subscriber on a subject receives every message
// (broadcast), unlike a queue where each message reaches one consumer.
func TestTopicsFanOut(t *testing.T) {
	tp := newTopics()
	ctx := context.Background()

	const subscribers = 3
	var wg sync.WaitGroup
	wg.Add(subscribers)
	got := make([]chan string, subscribers)
	for i := 0; i < subscribers; i++ {
		got[i] = make(chan string, 1)
		ch := got[i]
		sub, err := tp.Subscribe(ctx, "events", func(_ context.Context, m types.Message) error {
			s, _ := m.Body.(string)
			ch <- s
			wg.Done()
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe %d: %v", i, err)
		}
		defer func() { _ = sub.Close() }()
	}

	if err := tp.Publish(ctx, "events", bodyMessage("hi")); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("not every subscriber received the message")
	}
	for i, ch := range got {
		if s := <-ch; s != "hi" {
			t.Errorf("subscriber %d got %q, want hi", i, s)
		}
	}
}

// TestTopicsSubjectIsolation verifies a message goes only to subscribers of its
// subject.
func TestTopicsSubjectIsolation(t *testing.T) {
	tp := newTopics()
	ctx := context.Background()

	other := make(chan struct{}, 1)
	sub, err := tp.Subscribe(ctx, "other", func(_ context.Context, _ types.Message) error {
		other <- struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	if err := tp.Publish(ctx, "events", bodyMessage("hi")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case <-other:
		t.Fatal("a subscriber on another subject received the message")
	case <-time.After(200 * time.Millisecond):
	}
}

// TestTopicsCloseStopsDelivery verifies a closed subscription receives no further
// messages and Publish still succeeds with no live subscriber (at-most-once).
func TestTopicsCloseStopsDelivery(t *testing.T) {
	tp := newTopics()
	ctx := context.Background()

	delivered := make(chan struct{}, 1)
	sub, err := tp.Subscribe(ctx, "events", func(_ context.Context, _ types.Message) error {
		delivered <- struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if err := sub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := tp.Publish(ctx, "events", bodyMessage("hi")); err != nil {
		t.Fatalf("Publish after close: %v", err)
	}
	select {
	case <-delivered:
		t.Fatal("a closed subscription received a message")
	case <-time.After(200 * time.Millisecond):
	}
}
