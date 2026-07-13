package k8s

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/types"
)

// TestTopicsBroadcast verifies a plain (non-queue-group) subscription fans every
// published message out to every subscriber, even across separate subscriptions —
// the difference from the queues' competing-consumer delivery.
func TestTopicsBroadcast(t *testing.T) {
	tp := newNATSTopics(connect(t, runServer(t)), "dep-1")
	ctx := context.Background()

	const subscribers = 3
	var wg sync.WaitGroup
	wg.Add(subscribers)
	for i := 0; i < subscribers; i++ {
		sub, err := tp.Subscribe(ctx, "events", func(_ context.Context, _ types.Message) error {
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
	case <-time.After(3 * time.Second):
		t.Fatal("not every subscriber received the broadcast")
	}
}

// TestTopicsSubjectScopingIsolatesDeployments verifies two deployments sharing a
// broker do not see each other's topic messages on the same user subject.
func TestTopicsSubjectScopingIsolatesDeployments(t *testing.T) {
	url := runServer(t)
	a := newNATSTopics(connect(t, url), "dep-a")
	b := newNATSTopics(connect(t, url), "dep-b")
	ctx := context.Background()

	got := make(chan struct{}, 1)
	sub, err := a.Subscribe(ctx, "shared", func(_ context.Context, _ types.Message) error {
		got <- struct{}{}
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Close() }()

	if err := b.Publish(ctx, "shared", types.Message{}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case <-got:
		t.Fatal("deployment a received deployment b's topic message")
	case <-time.After(200 * time.Millisecond):
	}
}
