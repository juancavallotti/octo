package queue

import (
	"context"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// sent records one Publish or Request call for assertions.
type sent struct {
	subject string
	msg     types.Message
}

// fakeQueues records sends and answers Request with a canned reply. It is enough
// to drive the dispatch block without a real backend.
type fakeQueues struct {
	mu        sync.Mutex
	published []sent
	requested []sent
	reply     types.Message
	replyErr  error
}

func (q *fakeQueues) Publish(_ context.Context, subject string, msg types.Message) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.published = append(q.published, sent{subject: subject, msg: msg})
	return nil
}

func (q *fakeQueues) Request(
	_ context.Context, subject string, msg types.Message, _ ...core.RequestOption,
) (types.Message, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.requested = append(q.requested, sent{subject: subject, msg: msg})
	return q.reply, q.replyErr
}

//nolint:ireturn // satisfies core.Queues
func (q *fakeQueues) Subscribe(
	context.Context, string, core.QueueHandler, ...core.SubscribeOption,
) (core.Subscription, error) {
	return nil, nil
}

// fakeServices exposes only Queues; the dispatch block touches nothing else.
type fakeServices struct{ q core.Queues }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) LeaderElection() core.LeaderElection { return nil }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Leases() core.Leases { return core.NoopLeases() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) KV() core.KV { return nil }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Secrets() core.SecretStore { return nil }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Queues() core.Queues { return f.q }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Topics() core.Topics { return core.NoopTopics() }

//nolint:ireturn // satisfies the RuntimeServices interface
func (f fakeServices) Resources() core.ResourceLoader { return core.NoopResourceLoader{} }

//nolint:ireturn // implements core.RuntimeServices
func (f fakeServices) Traces() core.TracePublisher   { return core.NoopTracer() }
func (f fakeServices) AgentMemory() core.AgentMemory { return core.NoopAgentMemory() }

func (f fakeServices) Close() error { return nil }

// build builds the dispatch block from raw settings.
//
//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func build(t *testing.T, settings types.Settings) core.MessageProcessor {
	t.Helper()
	p, err := newDispatch(settings, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newDispatch: %v", err)
	}
	return p
}

// ctxWith returns a context carrying q as the runtime queue service.
func ctxWith(q core.Queues) context.Context {
	return core.ContextWithRuntimeServices(context.Background(), fakeServices{q: q})
}

func TestDispatchRequestFoldsReplyBack(t *testing.T) {
	reply := types.Message{Body: "enriched", Variables: types.Variables{"score": 42.0}}
	q := &fakeQueues{reply: reply}

	p := build(t, types.Settings{"subject": `"orders." + vars.region`, "awaitReply": true})

	msg, _ := types.NewMessage("corr-1")
	msg.Body = "raw"
	msg.Variables.Set("region", "eu")

	out, err := p.Process(ctxWith(q), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.Body != "enriched" {
		t.Fatalf("body = %v, want enriched", out.Body)
	}
	if score, _ := out.Variables.Float("score"); score != 42.0 {
		t.Fatalf("score = %v, want 42", score)
	}
	if len(q.requested) != 1 {
		t.Fatalf("requested %d times, want 1", len(q.requested))
	}
	if q.requested[0].subject != "orders.eu" {
		t.Fatalf("subject = %q, want orders.eu", q.requested[0].subject)
	}
	// The outgoing message is rekeyed so it correlates independently of this flow.
	if q.requested[0].msg.EventID == msg.EventID {
		t.Fatal("dispatched message was not rekeyed")
	}
}

func TestDispatchPublishesByDefaultAndLeavesMessage(t *testing.T) {
	q := &fakeQueues{}
	// No awaitReply: the default is fire-and-forget publish.
	p := build(t, types.Settings{"subject": `"audit"`})

	msg, _ := types.NewMessage("")
	msg.Body = "original"

	out, err := p.Process(ctxWith(q), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.Body != "original" {
		t.Fatalf("body = %v, want unchanged original", out.Body)
	}
	if len(q.published) != 1 {
		t.Fatalf("published %d times, want 1", len(q.published))
	}
	if len(q.requested) != 0 {
		t.Fatalf("default must not Request; got %d", len(q.requested))
	}
	if q.published[0].subject != "audit" {
		t.Fatalf("subject = %q, want audit", q.published[0].subject)
	}
}

func TestDispatchRequiresSubject(t *testing.T) {
	if _, err := newDispatch(types.Settings{}, core.BlockDeps{}); err == nil {
		t.Fatal("expected an error for missing subject")
	}
}
