package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// leaderElection reports a fixed leadership status, so a test can run the reaper
// as the leader or as a standby.
type leaderElection struct{ leader bool }

//nolint:ireturn // satisfies the LeaderElection interface
func (l leaderElection) Acquire(context.Context, string) (core.Leadership, error) {
	return leadership(l), nil
}

type leadership struct{ leader bool }

func (l leadership) IsLeader() bool { return l.leader }
func (l leadership) Close() error   { return nil }

// electedServices is fakeServices with a leadership status a test controls.
type electedServices struct {
	fakeServices
	leader bool
}

//nolint:ireturn // satisfies the RuntimeServices interface
func (e electedServices) LeaderElection() core.LeaderElection {
	return leaderElection{leader: e.leader}
}

// withElection returns a context whose services grant or withhold leadership.
func withElection(ctx context.Context, leader bool) context.Context {
	svc := electedServices{fakeServices: fakeServices{kv: newFakeKV()}, leader: leader}
	return core.ContextWithRuntimeServices(ctx, svc)
}

// buildAggregate builds an aggregate block wired to a recording continuation.
func buildAggregate(t *testing.T, cfg types.BlockConfig) (*aggregate, *recordingContinuation) {
	t.Helper()
	cfg.Type = blockKindAggregate
	proc, err := rootBuilder(testRegistry()).aggregateBlock(cfg)
	if err != nil {
		t.Fatalf("build aggregate: %v", err)
	}
	block, ok := proc.(*aggregate)
	if !ok {
		t.Fatalf("expected *aggregate, got %T", proc)
	}
	cont := &recordingContinuation{}
	block.SetContinuation(cont)
	return block, cont
}

// groupMessage builds a message as a split would have produced it.
func groupMessage(t *testing.T, groupID string, index, size int, body any) *types.Message {
	t.Helper()
	msg := mustMessage(t)
	msg.Body = body
	msg.Variables.Set(varGroupID, groupID)
	msg.Variables.Set(varGroupIndex, index)
	msg.Variables.Set(varGroupSize, size)
	return msg
}

// feed folds a series of messages through the block.
func feed(ctx context.Context, t *testing.T, block *aggregate, msgs ...*types.Message) {
	t.Helper()
	for i, msg := range msgs {
		if _, err := block.Process(ctx, msg); err != nil {
			t.Fatalf("Process message %d: %v", i, err)
		}
	}
}

// accOf reads the aggregated array off a released message.
func accOf(t *testing.T, msg *types.Message) []any {
	t.Helper()
	items, ok := msg.Body.([]any)
	if !ok {
		t.Fatalf("aggregated body is %T, want an array", msg.Body)
	}
	return items
}

func TestAggregatePairsWithSplitWithNoConfiguration(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{})

	feed(ctx, t, block,
		groupMessage(t, "g1", 0, 3, "a"),
		groupMessage(t, "g1", 1, 3, "b"),
	)
	if len(cont.resumed()) != 0 {
		t.Fatal("the group is not complete yet")
	}

	feed(ctx, t, block, groupMessage(t, "g1", 2, 3, "c"))

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want 1", len(released))
	}
	if got := accOf(t, released[0]); len(got) != 3 {
		t.Fatalf("aggregated %d items, want 3", len(got))
	}
	if reason, _ := released[0].Variables.String(varAggregateReason); reason != aggregateReasonSize {
		t.Errorf("reason = %q, want %q", reason, aggregateReasonSize)
	}
	if count, _ := released[0].Variables.Int(varAggregateCount); count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	if key, _ := released[0].Variables.String(varAggregateKey); key != "g1" {
		t.Errorf("key = %q, want the group id", key)
	}
}

func TestAggregateCompletesRegardlessOfArrivalOrder(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{})

	// Elements of a split run concurrently on separate workers and routinely
	// finish out of order. Completing on "I saw the last one" would close this
	// group after one message and silently drop the other two — which is exactly
	// why completion is a count and there is no groupLast variable.
	feed(ctx, t, block,
		groupMessage(t, "g1", 2, 3, "c"),
		groupMessage(t, "g1", 0, 3, "a"),
		groupMessage(t, "g1", 1, 3, "b"),
	)

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want exactly 1", len(released))
	}
	// And they are re-joined in the split's order, not the order they finished:
	// the same input must not yield a differently-shuffled array each run.
	if got := accOf(t, released[0]); len(got) != 3 ||
		got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Fatalf("aggregated %v, want [a b c] in the split's order", got)
	}
}

func TestAggregateAppendsUnpositionedMessagesInArrivalOrder(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{
		Correlation:    `"events"`,
		CompletionSize: "3",
	})

	// Events batched off a source were never a collection, so they carry no
	// position and arrival is the only order they have.
	for _, body := range []string{"first", "second", "third"} {
		msg := mustMessage(t)
		msg.Body = body
		feed(ctx, t, block, msg)
	}

	got := accOf(t, cont.resumed()[0])
	if len(got) != 3 || got[0] != "first" || got[1] != "second" || got[2] != "third" {
		t.Fatalf("aggregated %v, want arrival order", got)
	}
}

func TestAggregateLeavesAGapForAMissingElement(t *testing.T) {
	ctx := withElection(context.Background(), true)
	block, cont := buildAggregate(t, types.BlockConfig{CompletionTimeout: "10ms"})

	if err := block.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = block.Stop(context.Background()) })

	// Element 1 of 3 never arrives — it failed in its own invocation. The group
	// can only close on its timeout, and it reports the hole where the element
	// should have been rather than quietly renumbering the survivors.
	feed(ctx, t, block,
		groupMessage(t, "g1", 0, 3, "a"),
		groupMessage(t, "g1", 2, 3, "c"),
	)

	deadline := time.Now().Add(3 * time.Second)
	for len(cont.resumed()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	released := cont.resumed()
	if len(released) == 0 {
		t.Fatal("the group never completed on its timeout")
	}
	got := accOf(t, released[0])
	if len(got) != 3 || got[0] != "a" || got[1] != nil || got[2] != "c" {
		t.Fatalf("aggregated %v, want [a <nil> c] with the gap preserved", got)
	}
}

func TestAggregateCompletesAStreamingSplit(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{})

	// A streaming split reveals the size on the final element only, and that
	// element can arrive at any point. The group learns the size whenever it
	// shows up and completes once the count catches up.
	feed(ctx, t, block,
		groupMessage(t, "g1", 0, sizeUnknown, "a"),
		groupMessage(t, "g1", 2, 3, "c"),
		groupMessage(t, "g1", 1, sizeUnknown, "b"),
	)

	if len(cont.resumed()) != 1 {
		t.Fatalf("released %d groups, want 1", len(cont.resumed()))
	}
}

func TestAggregateKeepsGroupsApart(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{})

	feed(ctx, t, block,
		groupMessage(t, "g1", 0, 2, "a1"),
		groupMessage(t, "g2", 0, 2, "b1"),
		groupMessage(t, "g2", 1, 2, "b2"),
		groupMessage(t, "g1", 1, 2, "a2"),
	)

	released := cont.resumed()
	if len(released) != 2 {
		t.Fatalf("released %d groups, want 2", len(released))
	}
	// Each group's items must be only its own; g1 carries the "a" messages and g2
	// the "b" ones, interleaved arrival notwithstanding.
	prefixes := map[string]string{"g1": "a", "g2": "b"}
	for _, msg := range released {
		key, _ := msg.Variables.String(varAggregateKey)
		want, known := prefixes[key]
		if !known {
			t.Fatalf("unexpected group %q", key)
		}
		for _, item := range accOf(t, msg) {
			text, _ := item.(string)
			if !strings.HasPrefix(text, want) {
				t.Errorf("group %q contains %q from another group", key, text)
			}
		}
	}
}

func TestAggregateCompletionSizeAsAConstant(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{
		Correlation:    `"batch"`,
		CompletionSize: "2",
	})

	// completionSize is an expression, so a constant batch size and "however many
	// this split produced" are the same field.
	for i := range 5 {
		msg := mustMessage(t)
		msg.Body = float64(i)
		feed(ctx, t, block, msg)
	}

	released := cont.resumed()
	if len(released) != 2 {
		t.Fatalf("released %d batches of 2 from 5 messages, want 2", len(released))
	}
}

func TestAggregateCompletionExpressionSeesTheGroup(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{
		Correlation:          `"orders"`,
		CompletionExpression: "vars.group.count >= 2 && vars.group.acc.size() >= 2",
	})

	msgA := mustMessage(t)
	msgA.Body = "a"
	feed(ctx, t, block, msgA)
	if len(cont.resumed()) != 0 {
		t.Fatal("the predicate should not hold after one message")
	}

	msgB := mustMessage(t)
	msgB.Body = "b"
	feed(ctx, t, block, msgB)

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want 1", len(released))
	}
	if reason, _ := released[0].Variables.String(varAggregateReason); reason != aggregateReasonExpression {
		t.Errorf("reason = %q, want %q", reason, aggregateReasonExpression)
	}
}

func TestAggregateExpressionStrategyFolds(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{
		Correlation: `"totals"`,
		Strategy:    aggregateStrategyExpression,
		// The fold sees the group *before* this message, so acc is the previous
		// accumulator — null on the first message.
		Expression:     "(has(vars.group.acc) && vars.group.acc != null ? vars.group.acc : 0.0) + body.amount",
		CompletionSize: "3",
	})

	for _, amount := range []float64{10, 20, 30} {
		msg := mustMessage(t)
		msg.Body = map[string]any{"amount": amount}
		feed(ctx, t, block, msg)
	}

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want 1", len(released))
	}
	if total, ok := released[0].Body.(float64); !ok || total != 60 {
		t.Fatalf("folded total = %v, want 60", released[0].Body)
	}
}

func TestAggregateInheritsTheCallersCorrelationID(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{})

	first, err := types.NewMessage("caller-123")
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	first.Body = "a"
	first.Variables.Set(varGroupID, "g1")
	first.Variables.Set(varGroupSize, 2)

	feed(ctx, t, block, first, groupMessage(t, "g1", 1, 2, "b"))

	// The caller's correlation chain is not the aggregator's to break, so the
	// group inherits it rather than overwriting it with the group key.
	released := cont.resumed()[0]
	if released.CorrelationID != "caller-123" {
		t.Errorf("CorrelationID = %q, want the first message's", released.CorrelationID)
	}
}

func TestAggregateClearsGroupVariables(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{})

	feed(ctx, t, block,
		groupMessage(t, "g1", 0, 2, "a"),
		groupMessage(t, "g1", 1, 2, "b"),
	)

	// They described the group that just closed. Forwarding them would make a
	// second aggregation stage put every message in a batch of one.
	released := cont.resumed()[0]
	for _, name := range []string{varGroupID, varGroupIndex, varGroupSize} {
		if _, present := released.Variables[name]; present {
			t.Errorf("aggregated message still carries %q", name)
		}
	}
}

func TestAggregateAbsorbsTheMessageAndBuildsAResponse(t *testing.T) {
	response := types.FlowConfig{Process: []types.BlockConfig{{Type: "accepted"}}}
	reg := testRegistry()
	reg.MustRegister("accepted", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Body = "queued"
			return msg, nil
		}), nil
	})

	proc, err := rootBuilder(reg).aggregateBlock(types.BlockConfig{
		Type: blockKindAggregate, BuildResponse: &response,
	})
	if err != nil {
		t.Fatalf("build aggregate: %v", err)
	}
	block, _ := proc.(*aggregate)
	block.SetContinuation(&recordingContinuation{})

	ctx, _ := withFakeServices(context.Background())
	out, err := block.Process(ctx, groupMessage(t, "g1", 0, 5, "a"))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out.Body != "queued" {
		t.Errorf("response body = %v, want the buildResponse result", out.Body)
	}
	if !out.StopRequested() {
		t.Error("the absorbed message should stop the chain")
	}
}

func TestAggregateOverflowPolicies(t *testing.T) {
	tests := []struct {
		name        string
		onOverflow  string
		wantErr     bool
		wantGroups  int
		wantDropped bool
	}{
		{name: "fail surfaces the cap", onOverflow: aggregateOverflowFail, wantErr: true},
		{name: "drop discards the message", onOverflow: aggregateOverflowDrop, wantDropped: true},
		{name: "complete releases the oldest", onOverflow: aggregateOverflowComplete, wantGroups: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := withFakeServices(context.Background())
			block, cont := buildAggregate(t, types.BlockConfig{MaxGroups: 1, OnOverflow: tt.onOverflow})

			// One open group fills the cap; the second message wants a new one.
			feed(ctx, t, block, groupMessage(t, "g1", 0, 5, "a"))
			out, err := block.Process(ctx, groupMessage(t, "g2", 0, 5, "b"))

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected the cap to surface as an error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if tt.wantDropped && out != nil {
				t.Fatal("an overflow drop should drop the message")
			}
			if got := len(cont.resumed()); got != tt.wantGroups {
				t.Fatalf("released %d groups, want %d", got, tt.wantGroups)
			}
		})
	}
}

func TestAggregateOverflowReleasesTheOldestGroupWithoutATimeout(t *testing.T) {
	// onOverflow: complete does not require a completionTimeout — an aggregate can
	// complete on size or on a predicate alone — so the victim cannot be chosen by
	// deadline: every group would have none and the choice would fall to Go's
	// randomized map iteration. Repeated because that failure is probabilistic:
	// picking by chance lands on the oldest sometimes.
	for run := range 8 {
		ctx, _ := withFakeServices(context.Background())
		block, cont := buildAggregate(t, types.BlockConfig{
			MaxGroups: 3, OnOverflow: aggregateOverflowComplete,
		})

		// Three groups opened in order, none of them complete, none with a deadline.
		feed(ctx, t, block,
			groupMessage(t, "oldest", 0, 5, "a"),
			groupMessage(t, "middle", 0, 5, "b"),
			groupMessage(t, "newest", 0, 5, "c"),
		)
		if got := len(cont.resumed()); got != 0 {
			t.Fatalf("run %d: released %d groups before the cap was reached", run, got)
		}

		feed(ctx, t, block, groupMessage(t, "fourth", 0, 5, "d"))

		released := cont.resumed()
		if len(released) != 1 {
			t.Fatalf("run %d: released %d groups, want 1", run, len(released))
		}
		if key, _ := released[0].Variables.String(varAggregateKey); key != "oldest" {
			t.Fatalf("run %d: released group %q, want the oldest", run, key)
		}
		if reason, _ := released[0].Variables.String(varAggregateReason); reason != aggregateReasonOverflow {
			t.Errorf("run %d: reason = %q, want %q", run, reason, aggregateReasonOverflow)
		}
	}
}

func TestAggregateCapHoldsWhenGroupsOpenConcurrently(t *testing.T) {
	const (
		maxGroups = 5
		arrivals  = 50
	)
	ctx, _ := withFakeServices(context.Background())
	block, _ := buildAggregate(t, types.BlockConfig{
		MaxGroups: maxGroups, OnOverflow: aggregateOverflowDrop,
	})

	// Built up front: groupMessage reports failures with t.Fatalf, which is only
	// defined on the goroutine running the test.
	msgs := make([]*types.Message, arrivals)
	for i := range arrivals {
		msgs[i] = groupMessage(t, fmt.Sprintf("g%d", i), 0, 5, i)
	}

	// Every message opens a group of its own, all at once. Reading the count and
	// writing the group afterwards would let all fifty see the same empty index:
	// folds of *different* groups are deliberately not serialized against each
	// other, so nothing but an atomic claim bounds this.
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		admitted int
	)
	for _, msg := range msgs {
		wg.Add(1)
		go func(msg *types.Message) {
			defer wg.Done()
			out, err := block.Process(ctx, msg)
			if err != nil {
				t.Errorf("Process: %v", err)
				return
			}
			if out == nil {
				return // the overflow policy dropped it
			}
			mu.Lock()
			defer mu.Unlock()
			admitted++
		}(msg)
	}
	wg.Wait()

	if admitted != maxGroups {
		t.Errorf("admitted %d messages, want exactly maxGroups (%d)", admitted, maxGroups)
	}
}

func TestAggregateIgnoresAnImplausiblePosition(t *testing.T) {
	tests := []struct {
		name  string
		index int
		size  int
	}{
		// groupIndex is an ordinary message variable that anything upstream can
		// set. Padding to whatever it names would let one message allocate a slice
		// of that size under the fold lock and then encode it into the store.
		{name: "past the ceiling", index: maxPositionedIndex * 2, size: sizeUnknown},
		{name: "past the group's own size", index: 50, size: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := withFakeServices(context.Background())
			block, cont := buildAggregate(t, types.BlockConfig{
				Correlation: `"g"`, CompletionSize: "2",
			})

			out := groupMessage(t, "g", tt.index, tt.size, "a")
			feed(ctx, t, block, out, groupMessage(t, "g", 1, tt.size, "b"))

			released := cont.resumed()
			if len(released) != 1 {
				t.Fatalf("released %d groups, want 1", len(released))
			}
			// Appended rather than positioned: two messages in, two items out.
			if got := accOf(t, released[0]); len(got) != 2 {
				t.Fatalf("aggregated %d items from 2 messages", len(got))
			}
		})
	}
}

// refusingContinuation is a continuation that will not take a group, standing in
// for a flow that cannot accept the handover.
type refusingContinuation struct {
	recordingContinuation
	refuse error
}

func (r *refusingContinuation) Resume(ctx context.Context, msg *types.Message) error {
	if r.refuse != nil {
		return r.refuse
	}
	return r.recordingContinuation.Resume(ctx, msg)
}

func TestAggregateKeepsAGroupTheContinuationRefuses(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, _ := buildAggregate(t, types.BlockConfig{Correlation: `"g"`, CompletionSize: "2"})
	cont := &refusingContinuation{refuse: errors.New("nowhere to put it")}
	block.SetContinuation(cont)

	// Completing takes the group out of the store — it has to, or a message
	// arriving during the handover would fold into a group already on its way out.
	// So a refused delivery is the one moment the accumulated messages exist
	// nowhere else, and losing them there would lose them for good.
	unpositioned := func(body string) *types.Message {
		msg := mustMessage(t)
		msg.Body = body
		return msg
	}
	feed(ctx, t, block, unpositioned("a"))
	if _, err := block.Process(ctx, unpositioned("b")); err == nil {
		t.Fatal("a refused delivery should surface as an error")
	}

	// The group is back, so what it holds is still there to be delivered later.
	cont.refuse = nil
	feed(ctx, t, block, unpositioned("c"))

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want 1", len(released))
	}
	if got := accOf(t, released[0]); len(got) != 3 {
		t.Fatalf("aggregated %v, want the two put back plus the third", got)
	}
}

// foldingContinuation folds another message into the block while it is being
// handed a completed group.
type foldingContinuation struct {
	recordingContinuation
	block *aggregate
	next  *types.Message
	once  sync.Once
}

func (f *foldingContinuation) Resume(ctx context.Context, msg *types.Message) error {
	f.once.Do(func() { _, _ = f.block.Process(ctx, f.next) })
	return f.recordingContinuation.Resume(ctx, msg)
}

func TestAggregateDoesNotHoldTheStripeWhileDelivering(t *testing.T) {
	// Delivery is caller-runs, so the whole downstream flow can run on this
	// goroutine — and a flow containing another aggregate folds while it does.
	// Holding the stripe across that serializes every unrelated group hashing to
	// it, and deadlocks outright when the two collide, which is what these two
	// keys arrange.
	delivered, folded := collidingKeys(t)

	ctx, _ := withFakeServices(context.Background())
	block, _ := buildAggregate(t, types.BlockConfig{CompletionSize: "2"})
	cont := &foldingContinuation{block: block, next: groupMessage(t, folded, 0, 2, "x")}
	block.SetContinuation(cont)

	feed(ctx, t, block,
		groupMessage(t, delivered, 0, 2, "a"),
		groupMessage(t, delivered, 1, 2, "b"),
	)

	if got := len(cont.resumed()); got != 1 {
		t.Fatalf("released %d groups, want 1", got)
	}
}

func TestAggregateOverflowEvictsAcrossASharedStripe(t *testing.T) {
	// Fold stripes are shared by design, so an eviction can land on the stripe of
	// the very message that triggered it. Admission therefore runs before the
	// stripe is taken; doing it under the lock deadlocks outright here, since a
	// sync.Mutex is not reentrant.
	victim, trigger := collidingKeys(t)

	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{
		MaxGroups: 1, OnOverflow: aggregateOverflowComplete,
	})

	feed(ctx, t, block, groupMessage(t, victim, 0, 5, "a"))
	feed(ctx, t, block, groupMessage(t, trigger, 0, 5, "b"))

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want 1", len(released))
	}
	if key, _ := released[0].Variables.String(varAggregateKey); key != victim {
		t.Errorf("released group %q, want %q", key, victim)
	}
}

// collidingKeys finds two correlation keys whose groups share a fold stripe.
func collidingKeys(t *testing.T) (victim, trigger string) {
	t.Helper()
	victim = "collide-0"
	stripe := stripeOf(hashedKey(victim))
	for i := 1; i < 10_000; i++ {
		candidate := fmt.Sprintf("collide-%d", i)
		if stripeOf(hashedKey(candidate)) == stripe {
			return victim, candidate
		}
	}
	t.Fatalf("no key sharing a stripe with %q in 10000 candidates", victim)
	return "", ""
}

func TestAggregateReaperCompletesOnTimeout(t *testing.T) {
	ctx := withElection(context.Background(), true)
	block, cont := buildAggregate(t, types.BlockConfig{CompletionTimeout: "10ms"})

	if err := block.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = block.Stop(context.Background()) })

	// Two of an expected five: nothing else is coming, so only the timeout can
	// close this group. It is the one condition that fires without a message.
	feed(ctx, t, block,
		groupMessage(t, "g1", 0, 5, "a"),
		groupMessage(t, "g1", 1, 5, "b"),
	)

	deadline := time.Now().Add(3 * time.Second)
	for len(cont.resumed()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want the timed-out one", len(released))
	}
	if reason, _ := released[0].Variables.String(varAggregateReason); reason != aggregateReasonTimeout {
		t.Errorf("reason = %q, want %q", reason, aggregateReasonTimeout)
	}
	if got := accOf(t, released[0]); len(got) != 2 {
		t.Errorf("timed-out group holds %d items, want the 2 that arrived", len(got))
	}
}

func TestAggregateSweepOnlyRunsOnTheLeader(t *testing.T) {
	tests := []struct {
		name     string
		leader   bool
		released int
	}{
		{name: "a standby leaves the group alone", leader: false, released: 0},
		{name: "the leader reaps it", leader: true, released: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := withElection(context.Background(), tt.leader)
			block, cont := buildAggregate(t, types.BlockConfig{CompletionTimeout: "10ms"})

			if err := block.Start(ctx); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { _ = block.Stop(context.Background()) })

			feed(ctx, t, block, groupMessage(t, "g1", 0, 5, "a"))
			time.Sleep(20 * time.Millisecond) // past the group's deadline

			// Driven straight rather than waited on: reapInterval clamps to a second,
			// so a test that slept for less than one tick would pass with the
			// leadership gate deleted. Both halves are asserted for the same reason —
			// "nothing was released" only means something if the leader releases.
			block.sweep(ctx)

			if got := len(cont.resumed()); got != tt.released {
				t.Fatalf("released %d groups, want %d", got, tt.released)
			}
		})
	}
}

func TestAggregateWithoutATimeoutStartsNoReaper(t *testing.T) {
	ctx := withElection(context.Background(), true)
	block, _ := buildAggregate(t, types.BlockConfig{})

	if err := block.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Nothing to fire on a timer means nothing exclusive to elect a leader for.
	if block.lease != nil {
		t.Error("an aggregate with no timeout should not campaign for leadership")
	}
	if err := block.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
}

func TestAggregateFoldsConcurrentlyExactlyOnce(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, cont := buildAggregate(t, types.BlockConfig{})

	// Replicas fold into the same group at the same time. The optimistic
	// read-modify-write is what makes this safe: a version conflict means someone
	// folded first, so the retry folds onto their result instead of overwriting it.
	const n = 20
	// Built up front: groupMessage reports failures with t.Fatalf, which is only
	// defined on the goroutine running the test.
	msgs := make([]*types.Message, n)
	for i := range n {
		msgs[i] = groupMessage(t, "g1", i, n, i)
	}

	var wg sync.WaitGroup
	wg.Add(n)
	for _, msg := range msgs {
		go func(msg *types.Message) {
			defer wg.Done()
			if _, err := block.Process(ctx, msg); err != nil {
				t.Errorf("Process: %v", err)
			}
		}(msg)
	}
	wg.Wait()

	released := cont.resumed()
	if len(released) != 1 {
		t.Fatalf("released %d groups, want exactly 1", len(released))
	}
	if got := accOf(t, released[0]); len(got) != n {
		t.Fatalf("aggregated %d items, want all %d", len(got), n)
	}
}

func TestAggregateStoreKeyDefaultsToTheBlockPath(t *testing.T) {
	explicit, _ := buildAggregate(t, types.BlockConfig{StoreKey: "order-totals"})
	if explicit.StateKey() != "order-totals" {
		t.Errorf("explicit storeKey = %q", explicit.StateKey())
	}
	if explicit.leaderKey != "aggregate_order-totals" {
		// The leader key must derive from the same identity as the stored state, or
		// two generations of a reloaded flow would hold two leases over one set of
		// groups and reap them twice.
		t.Errorf("leaderKey = %q, want it derived from the storeKey", explicit.leaderKey)
	}

	defaulted, _ := buildAggregate(t, types.BlockConfig{})
	if defaulted.StateKey() != "test" {
		t.Errorf("default storeKey = %q, want the block's address", defaulted.StateKey())
	}
}

func TestAggregateStoreKeyIsolatesTwoBlocks(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	first, firstCont := buildAggregate(t, types.BlockConfig{StoreKey: "one"})
	second, secondCont := buildAggregate(t, types.BlockConfig{StoreKey: "two"})

	// Same correlation key, different storeKeys: two independent groups, neither
	// completing on the other's messages.
	feed(ctx, t, first, groupMessage(t, "g1", 0, 2, "a"))
	feed(ctx, t, second, groupMessage(t, "g1", 0, 2, "b"))
	if len(firstCont.resumed()) != 0 || len(secondCont.resumed()) != 0 {
		t.Fatal("neither group should be complete")
	}

	feed(ctx, t, first, groupMessage(t, "g1", 1, 2, "a2"))
	if len(firstCont.resumed()) != 1 {
		t.Fatal("the first block's group should have completed")
	}
	if len(secondCont.resumed()) != 0 {
		t.Fatal("the second block's group must not be affected")
	}
}

func TestAggregateRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  types.BlockConfig
	}{
		{name: "expression strategy without an expression", cfg: types.BlockConfig{Strategy: "expression"}},
		{name: "append strategy with an expression", cfg: types.BlockConfig{Expression: "body"}},
		{name: "unknown strategy", cfg: types.BlockConfig{Strategy: "sideways"}},
		{name: "unparseable timeout", cfg: types.BlockConfig{CompletionTimeout: "soon"}},
		{name: "non-positive timeout", cfg: types.BlockConfig{CompletionTimeout: "0s"}},
		{name: "unknown timeoutFrom", cfg: types.BlockConfig{TimeoutFrom: "middle"}},
		{name: "unknown onOverflow", cfg: types.BlockConfig{OnOverflow: "explode"}},
		{name: "bad correlation expression", cfg: types.BlockConfig{Correlation: "body."}},
		{name: "foreign slot", cfg: types.BlockConfig{Condition: "true"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Type = blockKindAggregate
			if _, err := rootBuilder(testRegistry()).aggregateBlock(tt.cfg); err == nil {
				t.Fatal("expected a build error")
			}
		})
	}
}

func TestAggregateRejectsBeingNested(t *testing.T) {
	nested := rootBuilder(testRegistry()).branch(core.BranchBody)
	_, err := nested.aggregateBlock(types.BlockConfig{Type: blockKindAggregate})
	if err == nil {
		t.Fatal("an aggregate inside a composite slot must fail the build")
	}
}

func TestAggregateNeedsACorrelationForNonSplitMessages(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	block, _ := buildAggregate(t, types.BlockConfig{})

	// The default correlation reads the variable a split sets. A message from
	// anywhere else has none, and the error should say what to do about it.
	msg := mustMessage(t)
	msg.Body = "a"
	_, err := block.Process(ctx, msg)
	if err == nil {
		t.Fatal("expected an error when the correlation variable is absent")
	}
	if !strings.Contains(err.Error(), "correlation") {
		t.Fatalf("error should point at the correlation setting, got %q", err)
	}
}
