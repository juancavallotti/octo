package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fakeTopics is a minimal in-memory core.Topics for tests: it records publishes and
// hands delivered messages to the registered handlers, so a test can drive both the
// publish-event block and the event source without a real backend.
type fakeTopics struct {
	mu        sync.Mutex
	published []publishedEvent
	handlers  map[string][]core.TopicHandler
}

type publishedEvent struct {
	subject string
	msg     types.Message
}

func newFakeTopics() *fakeTopics {
	return &fakeTopics{handlers: make(map[string][]core.TopicHandler)}
}

func (f *fakeTopics) Publish(_ context.Context, subject string, msg types.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.published = append(f.published, publishedEvent{subject: subject, msg: msg})
	return nil
}

//nolint:ireturn // satisfies core.Topics
func (f *fakeTopics) Subscribe(
	_ context.Context, subject string, handler core.TopicHandler, _ ...core.SubscribeOption,
) (core.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[subject] = append(f.handlers[subject], handler)
	return fakeSubscription{}, nil
}

// deliver invokes every handler registered for subject with msg.
func (f *fakeTopics) deliver(ctx context.Context, subject string, msg types.Message) {
	f.mu.Lock()
	handlers := append([]core.TopicHandler(nil), f.handlers[subject]...)
	f.mu.Unlock()
	for _, h := range handlers {
		_ = h(ctx, msg)
	}
}

type fakeSubscription struct{}

func (fakeSubscription) Close() error { return nil }

// fakeServices wires fakeTopics into the RuntimeServices contract; every other
// capability is the no-op.
type fakeServices struct{ topics *fakeTopics }

//nolint:ireturn // satisfies core.RuntimeServices
func (f fakeServices) LeaderElection() core.LeaderElection { return core.NoopLeaderElection() }

//nolint:ireturn // satisfies core.RuntimeServices
func (f fakeServices) Leases() core.Leases { return core.NoopLeases() }

//nolint:ireturn // satisfies core.RuntimeServices
func (f fakeServices) KV() core.KV { return core.NoopRuntimeServices().KV() }

//nolint:ireturn // satisfies core.RuntimeServices
func (f fakeServices) Secrets() core.SecretStore { return core.NoopRuntimeServices().Secrets() }

//nolint:ireturn // satisfies core.RuntimeServices
func (f fakeServices) Queues() core.Queues { return core.NoopQueues() }

//nolint:ireturn // satisfies core.RuntimeServices
func (f fakeServices) Topics() core.Topics { return f.topics }

//nolint:ireturn // satisfies core.RuntimeServices
func (f fakeServices) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

//nolint:ireturn // implements core.RuntimeServices
func (f fakeServices) Traces() core.TracePublisher   { return core.NoopTracer() }
func (f fakeServices) AgentMemory() core.AgentMemory { return core.NoopAgentMemory() }

func (f fakeServices) Close() error { return nil }

func contextWithTopics(t *fakeTopics) context.Context {
	return core.ContextWithRuntimeServices(context.Background(), fakeServices{topics: t})
}

func mustMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	msg.Body = body
	return msg
}

func TestPublishEventBroadcastsBody(t *testing.T) {
	ft := newFakeTopics()
	ctx := contextWithTopics(ft)

	proc, err := newPublish(types.Settings{"subject": `"orders." + body.region`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newPublish: %v", err)
	}
	out, err := proc.Process(ctx, mustMessage(t, map[string]any{"region": "us", "id": "7"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.Body.(map[string]any)["id"] != "7" {
		t.Errorf("current message body was mutated: %v", out.Body)
	}
	if len(ft.published) != 1 {
		t.Fatalf("published %d events, want 1", len(ft.published))
	}
	got := ft.published[0]
	if got.subject != "orders.us" {
		t.Errorf("subject = %q, want orders.us", got.subject)
	}
	if got.msg.Body.(map[string]any)["id"] != "7" {
		t.Errorf("published body = %v, want the message body", got.msg.Body)
	}
	if got.msg.EventID == out.EventID {
		t.Error("published message should be rekeyed to its own EventID")
	}
}

func TestPublishEventValueExpression(t *testing.T) {
	ft := newFakeTopics()
	ctx := contextWithTopics(ft)

	proc, err := newPublish(
		types.Settings{"subject": `"events"`, "value": `{"n": body.n * 2.0}`},
		core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("newPublish: %v", err)
	}
	if _, err := proc.Process(ctx, mustMessage(t, map[string]any{"n": float64(21)})); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if len(ft.published) != 1 {
		t.Fatalf("published %d events, want 1", len(ft.published))
	}
	if got := ft.published[0].msg.Body.(map[string]any)["n"]; got != float64(42) {
		t.Errorf("published n = %v, want 42", got)
	}
}

func TestPublishEventBuildValidation(t *testing.T) {
	tests := []struct {
		name string
		raw  types.Settings
	}{
		{name: "no subject", raw: nil},
		{name: "bad subject expr", raw: types.Settings{"subject": "body."}},
		{name: "bad value expr", raw: types.Settings{"subject": `"s"`, "value": "body."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := newPublish(tt.raw, core.BlockDeps{}); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestEventSourceForwardsDeliveries(t *testing.T) {
	ft := newFakeTopics()
	ctx := contextWithTopics(ft)

	out := make(chan *types.Message, 1)
	conn := &Connector{}
	src, err := conn.NewSource(
		types.SourceConfig{Type: "event", Settings: types.Settings{"subject": "orders"}},
		out,
		core.SourceDeps{},
	)
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	if err := src.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = src.Stop(context.Background()) }()

	in := mustMessage(t, map[string]any{"id": "7"})
	ft.deliver(ctx, "orders", *in)

	select {
	case got := <-out:
		if got.Body.(map[string]any)["id"] != "7" {
			t.Errorf("forwarded body = %v, want the delivered body", got.Body)
		}
		if got.EventID == in.EventID {
			t.Error("forwarded message should be rekeyed to its own EventID")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("source did not forward the delivered message")
	}
}

func TestEventSourceRequiresSubject(t *testing.T) {
	conn := &Connector{}
	if _, err := conn.NewSource(types.SourceConfig{Type: "event"}, make(chan *types.Message), core.SourceDeps{}); err == nil {
		t.Error("expected an error when the source has no subject")
	}
}
