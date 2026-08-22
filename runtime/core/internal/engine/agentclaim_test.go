package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The fakes below stand in for a deployment rather than for a process: one
// leases and one queues, shared by two registries that are otherwise unaware of
// each other. That is exactly the shape the real thing has — two replicas, one
// broker, one API server — and it is the only way to test a claim that only
// means anything between processes.

// sharedLeases is an exclusive claim on a name, with no expiry: these tests are
// about who owns a conversation, and the modules' own tests cover the clock.
type sharedLeases struct {
	mu   sync.Mutex
	held map[string]*sharedLease
}

func newSharedLeases() *sharedLeases { return &sharedLeases{held: map[string]*sharedLease{}} }

//nolint:ireturn // satisfies core.Leases
func (s *sharedLeases) Acquire(
	_ context.Context, name string, _ ...core.LeaseOption,
) (core.Lease, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, taken := s.held[name]; taken {
		return nil, false, nil
	}
	lease := &sharedLease{owner: s, name: name, done: make(chan struct{})}
	s.held[name] = lease
	return lease, true, nil
}

// revoke takes a claim away from its holder, as an expiry or a failed renewal
// would.
func (s *sharedLeases) revoke(name string) {
	s.mu.Lock()
	lease := s.held[name]
	delete(s.held, name)
	s.mu.Unlock()
	if lease != nil {
		lease.lose()
	}
}

type sharedLease struct {
	owner *sharedLeases
	name  string
	done  chan struct{}
	once  sync.Once
}

func (l *sharedLease) Done() <-chan struct{} { return l.done }

func (l *sharedLease) Close() error {
	l.owner.mu.Lock()
	if l.owner.held[l.name] == l {
		delete(l.owner.held, l.name)
	}
	l.owner.mu.Unlock()
	l.lose()
	return nil
}

func (l *sharedLease) lose() { l.once.Do(func() { close(l.done) }) }

// sharedQueues delivers a request straight to whoever subscribed, and errors
// when nobody has — which is what core NATS does, and the distinction the whole
// delivery path is built on.
type sharedQueues struct {
	mu       sync.Mutex
	handlers map[string]core.QueueHandler
}

func newSharedQueues() *sharedQueues { return &sharedQueues{handlers: map[string]core.QueueHandler{}} }

var errNoSubscriber = errors.New("no responder")

func (q *sharedQueues) Publish(context.Context, string, types.Message) error { return nil }

func (q *sharedQueues) Request(
	ctx context.Context, subject string, msg types.Message, _ ...core.RequestOption,
) (types.Message, error) {
	q.mu.Lock()
	handler, ok := q.handlers[subject]
	q.mu.Unlock()
	if !ok {
		return types.Message{}, errNoSubscriber
	}
	return handler(ctx, msg)
}

//nolint:ireturn // satisfies core.Queues
func (q *sharedQueues) Subscribe(
	_ context.Context, subject string, handler core.QueueHandler, _ ...core.SubscribeOption,
) (core.Subscription, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.handlers[subject] = handler
	return &sharedSubscription{owner: q, subject: subject}, nil
}

type sharedSubscription struct {
	owner   *sharedQueues
	subject string
}

func (s *sharedSubscription) Close() error {
	s.owner.mu.Lock()
	defer s.owner.mu.Unlock()
	delete(s.owner.handlers, s.subject)
	return nil
}

// clusterServices is one deployment's worth of services: a shared claim and a
// shared broker, handed to more than one registry.
type clusterServices struct {
	fakeServices
	leases core.Leases
	broker core.Queues
}

//nolint:ireturn // satisfies core.RuntimeServices
func (c clusterServices) Leases() core.Leases { return c.leases }

//nolint:ireturn // satisfies core.RuntimeServices
func (c clusterServices) Queues() core.Queues { return c.broker }

// cluster is a deployment two registries run in.
type cluster struct {
	ctx    context.Context
	leases *sharedLeases
	// a and b are two replicas' registries. Nothing connects them but the
	// services above, which is the point.
	a, b *runRegistry
}

func newCluster(t *testing.T) *cluster {
	t.Helper()
	leases := newSharedLeases()
	svc := clusterServices{
		fakeServices: fakeServices{kv: newFakeKV()},
		leases:       leases,
		broker:       newSharedQueues(),
	}
	return &cluster{
		ctx:    core.ContextWithRuntimeServices(context.Background(), svc),
		leases: leases,
		a:      &runRegistry{runs: map[string]*agentRun{}},
		b:      &runRegistry{runs: map[string]*agentRun{}},
	}
}

// waitUntil blocks until cond holds, so a test can assert on something a goroutine
// sets without racing it or sleeping a guessed amount.
func waitUntil(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		case <-time.After(time.Millisecond):
		}
	}
}

// conversation is the one claim key these tests use. What varies between them is
// who owns it and what reaches it, never which conversation it is.
const conversation = "k"

// claimOn runs one claim against a replica's registry.
func (c *cluster) claimOn(t *testing.T, r *runRegistry, text string, run *agentRun) (*heldClaim, bool) {
	t.Helper()
	held, handedOff, err := r.offerOrClaim(c.ctx, conversation, text, run)
	if err != nil {
		t.Fatalf("offerOrClaim(%q): %v", conversation, err)
	}
	return held, handedOff
}

func TestOnlyOneReplicaClaimsAConversation(t *testing.T) {
	c := newCluster(t)
	first, second := &agentRun{}, &agentRun{}

	held, handedOff := c.claimOn(t, c.a, "hello", first)
	if handedOff || held == nil {
		t.Fatal("the first replica did not claim the conversation")
	}
	defer held.close()

	if _, handedOff = c.claimOn(t, c.b, "follow-up", second); !handedOff {
		t.Fatal("the second replica started its own run on a claimed conversation")
	}
	if c.b.runs[conversation] != nil {
		t.Error("the second replica registered a run for a conversation it does not own")
	}
}

// The follow-up has to arrive, not merely be refused a run of its own. A handover
// that reported success without delivering would lose the message and stop the
// sender's flow on the strength of it.
func TestAFollowUpOnAnotherReplicaReachesTheRun(t *testing.T) {
	c := newCluster(t)
	run := &agentRun{}

	held, _ := c.claimOn(t, c.a, "hello", run)
	defer held.close()

	if _, handedOff := c.claimOn(t, c.b, "actually, focus on pricing", &agentRun{}); !handedOff {
		t.Fatal("the follow-up was not handed over")
	}
	got := run.take()
	if len(got) != 1 || got[0] != "actually, focus on pricing" {
		t.Errorf("pending on the running replica = %+v, want the follow-up", got)
	}
}

// The one that matters most. A stop that lands on the wrong replica used to stop
// nothing and report success, which is indistinguishable from working.
func TestAStopOnAnotherReplicaEndsTheRun(t *testing.T) {
	c := newCluster(t)
	run := &agentRun{}

	held, _ := c.claimOn(t, c.a, "hello", run)
	defer held.close()

	if stopped := c.b.stop(c.ctx, conversation); !stopped {
		t.Fatal("a stop from another replica reported that nothing was running")
	}
	if !run.stopRequested() {
		t.Error("the run on the other replica was not asked to stop")
	}
}

// The other half of the same honesty: with nothing running anywhere, a stop must
// say so rather than claim a success it cannot have had.
func TestAStopWithNothingRunningAnywhereReportsFalse(t *testing.T) {
	c := newCluster(t)
	if stopped := c.b.stop(c.ctx, conversation); stopped {
		t.Error("a stop reported that it ended a run when none existed")
	}
}

func TestReleasingAClaimFreesItForAnotherReplica(t *testing.T) {
	c := newCluster(t)
	first := &agentRun{}

	held, _ := c.claimOn(t, c.a, "hello", first)
	first.close()
	held.close()
	c.a.release(conversation, first)

	second := &agentRun{}
	held2, handedOff := c.claimOn(t, c.b, "a new question", second)
	if handedOff {
		t.Fatal("a released conversation was still reported as claimed")
	}
	if held2 == nil {
		t.Fatal("the second replica did not take the released claim")
	}
	held2.close()
}

// A holder that has stopped accepting but has not yet released is the awkward
// middle: the name is on its way to being free, and giving up on it would drop a
// message somebody is waiting on.
func TestAClaimHeldByAFinishedRunIsRetried(t *testing.T) {
	c := newCluster(t)
	finishing := &agentRun{}

	held, _ := c.claimOn(t, c.a, "hello", finishing)
	finishing.close() // stopped accepting; the claim is still held

	// Release it from under the retry loop, as the finishing run's own deferred
	// cleanup would.
	go func() {
		held.close()
		c.a.release(conversation, finishing)
	}()

	successor := &agentRun{}
	held2, handedOff := c.claimOn(t, c.b, "a new question", successor)
	if handedOff {
		t.Fatal("a message was handed to a run that had stopped accepting")
	}
	if held2 == nil {
		t.Fatal("the successor never got the claim")
	}
	held2.close()
}

// A claim taken away — an expired lease, a renewal that did not land — means
// another replica now owns the conversation and will save it. Carrying on would
// be two runs writing one transcript, where the later save wins entirely.
func TestLosingTheClaimStopsTheRun(t *testing.T) {
	c := newCluster(t)
	run := &agentRun{}

	held, _ := c.claimOn(t, c.a, "hello", run)
	watchClaim(held, run, "worker", "t1")

	c.leases.revoke(conversation)

	// watchClaim acts on a goroutine, so wait for the flag rather than reading it
	// once and racing the scheduler.
	waitUntil(t, run.stopRequested, "the run to be stopped after losing its claim")
}

// A conversation nobody can be reached on is worse than one nobody owns: every
// follow-up would be refused delivery and every stop would silently fail. The
// claim is given back rather than held.
func TestAClaimIsGivenBackWhenItCannotBeSubscribedFor(t *testing.T) {
	c := newCluster(t)
	ctx := core.ContextWithRuntimeServices(context.Background(), clusterServices{
		fakeServices: fakeServices{kv: newFakeKV()},
		leases:       c.leases,
		broker:       brokenQueues{},
	})

	if _, _, err := c.a.offerOrClaim(ctx, conversation, "hello", &agentRun{}); err == nil {
		t.Fatal("claiming a conversation it could not subscribe for reported success")
	}
	if _, ok, _ := c.leases.Acquire(context.Background(), conversation); !ok {
		t.Error("the claim was kept after the subscription failed")
	}
}

// brokenQueues cannot be subscribed to.
type brokenQueues struct{ core.Queues }

//nolint:ireturn // satisfies core.Queues
func (brokenQueues) Subscribe(
	context.Context, string, core.QueueHandler, ...core.SubscribeOption,
) (core.Subscription, error) {
	return nil, errNoSubscriber
}

// A run that wedges must not own its conversation until the process restarts.
// With a cluster-wide claim that would take the conversation out of service for
// the whole deployment rather than for one replica.
func TestARunThatHoldsItsConversationTooLongIsStopped(t *testing.T) {
	run := &agentRun{}
	boundRunAge(run, "worker", "t1", time.Millisecond)

	waitUntil(t, run.stopRequested, "a wedged run to be stopped by its age bound")
}

// The bound is cancelled by an ordinary ending, so a run that answered is not
// stopped after the fact — which would mark a finished conversation as one
// somebody interrupted.
func TestTheAgeBoundIsCancelledWhenARunEndsNormally(t *testing.T) {
	run := &agentRun{}
	cancel := boundRunAge(run, "worker", "t1", time.Hour)

	if !cancel() {
		t.Fatal("the age bound had already fired on an hour-long limit")
	}
	if run.stopRequested() {
		t.Error("the run was stopped by a bound that was cancelled")
	}
}
