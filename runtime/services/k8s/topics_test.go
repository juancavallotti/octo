package k8s

import (
	"context"
	"encoding/json"
	"strings"
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

// A system: subject goes on the wire verbatim. Asserted directly, because silently
// inheriting the octo.<id>.t. prefix is the exact bug the convention exists to
// prevent — and it would look like a working publish that nobody ever receives.
func TestTopicsSystemPrefixOptsOutOfScoping(t *testing.T) {
	tp := newNATSTopics(nil, "dep-1")

	if got := tp.subject("events"); got != "octo.dep-1.t.events" {
		t.Errorf("ordinary subject = %q, want it scoped to the deployment", got)
	}
	if got := tp.subject("system:internal.integrations.events"); got != "internal.integrations.events" {
		t.Errorf("system subject = %q, want the prefix stripped and nothing added", got)
	}
	// Only the leading prefix counts; a subject that merely contains the word is
	// still this deployment's.
	if got := tp.subject("my.system:thing"); got != "octo.dep-1.t.my.system:thing" {
		t.Errorf("subject = %q, want only a leading system: to opt out", got)
	}
}

// The inverse of TestTopicsSubjectScopingIsolatesDeployments: on a system subject
// two deployments are deliberately on the same wire, which is the whole point.
func TestTopicsSystemSubjectCrossesDeployments(t *testing.T) {
	url := runServer(t)
	a := newNATSTopics(connect(t, url), "dep-a")
	b := newNATSTopics(connect(t, url), "dep-b")
	ctx := context.Background()

	// Subscribed with a raw client, because a flow may not subscribe to a system
	// subject — the platform is the consumer here, and it is not a flow.
	sub, err := a.conn.SubscribeSync("internal.integrations.events")
	if err != nil {
		t.Fatalf("SubscribeSync: %v", err)
	}
	if err := a.conn.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if err := b.Publish(ctx, "system:internal.integrations.events", bodyMessage("hi")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	m, err := sub.NextMsg(3 * time.Second)
	if err != nil {
		t.Fatalf("a system subject published by one deployment did not reach a listener outside it: %v", err)
	}
	if string(m.Data) != `"hi"` {
		t.Errorf("payload = %s, want the body", m.Data)
	}
}

// The prefix buys a flow the ability to raise a platform event, not to read the
// platform's traffic. internal.logs and internal.traces carry every deployment's
// records, so a flow that could subscribe there would be reading workloads it has
// nothing to do with — including through a wildcard.
func TestTopicsRefusesToSubscribeToASystemSubject(t *testing.T) {
	tp := newNATSTopics(connect(t, runServer(t)), "dep-a")

	for _, subject := range []string{"system:internal.logs", "system:internal.traces", "system:>"} {
		sub, err := tp.Subscribe(context.Background(), subject,
			func(context.Context, types.Message) error { return nil })
		if err == nil {
			_ = sub.Close()
			t.Errorf("want %q refused", subject)
			continue
		}
		if !strings.Contains(err.Error(), "published to, not subscribed to") {
			t.Errorf("error for %q = %v, want it to say why", subject, err)
		}
	}

	// An ordinary subject still subscribes, scoped to the deployment as before.
	sub, err := tp.Subscribe(context.Background(), "events",
		func(context.Context, types.Message) error { return nil })
	if err != nil {
		t.Fatalf("an ordinary subject should still subscribe: %v", err)
	}
	_ = sub.Close()
}

// What reaches the platform is the payload, and the platform hands it to a browser
// that JSON.parses it. So the payload has to be the body and nothing else — no
// envelope, no wrapper. The message's other fields ride in headers, which the
// platform ignores.
func TestTopicsSystemPublishPayloadIsTheBodyAsJSON(t *testing.T) {
	url := runServer(t)
	tp := newNATSTopics(connect(t, url), "dep-a")
	raw := connect(t, url)

	sub, err := raw.SubscribeSync("internal.integrations.events")
	if err != nil {
		t.Fatalf("SubscribeSync: %v", err)
	}
	if err := raw.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	event := map[string]any{"type": "integration.updated", "id": "abc", "name": "Demo"}
	if err := tp.Publish(context.Background(), "system:internal.integrations.events",
		types.Message{Body: event}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	m, err := sub.NextMsg(3 * time.Second)
	if err != nil {
		t.Fatalf("NextMsg: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(m.Data, &decoded); err != nil {
		t.Fatalf("the payload is not the JSON a browser would parse: %v (%s)", err, m.Data)
	}
	for key, want := range event {
		if decoded[key] != want {
			t.Errorf("payload[%q] = %v, want %v", key, decoded[key], want)
		}
	}
	if len(decoded) != len(event) {
		t.Errorf("payload = %v, want the body verbatim with nothing added", decoded)
	}
}
