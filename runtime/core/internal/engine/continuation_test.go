package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// takeover is a test processor that takes over the flow: it records the
// continuation the builder hands it and stops its own chain.
type takeover struct{ cont core.Continuation }

func (p *takeover) SetContinuation(c core.Continuation) { p.cont = c }

func (p *takeover) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	msg.RequestStop()
	return msg, nil
}

// continuationRegistry returns a registry with a "takeover" block whose single
// instance the caller can inspect, plus a "mark" block that appends its
// configured letter to the body so a tail's contents are observable.
func continuationRegistry(t testing.TB) (*core.BlockRegistry, *takeover) {
	t.Helper()
	reg := testRegistry()
	instance := &takeover{}
	reg.MustRegister("takeover", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return instance, nil
	})
	reg.MustRegister("mark", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		letter, _ := s.String("letter")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			body, _ := msg.Body.(string)
			msg.Body = body + letter
			return msg, nil
		}), nil
	})
	return reg, instance
}

// rootBuilder builds the way the runtime does — root chain, continuations wired.
func rootBuilder(reg *core.BlockRegistry) *builder {
	return &builder{reg: reg, pool: pool.New(0, 0), flowName: "test", path: "test", root: true}
}

func TestContinuationRunsTheRestOfTheChain(t *testing.T) {
	reg, block := continuationRegistry(t)

	flow, err := rootBuilder(reg).flow(types.FlowConfig{Process: []types.BlockConfig{
		{Type: "takeover"},
		{Type: "mark", Settings: types.Settings{"letter": "A"}},
		{Type: "mark", Settings: types.Settings{"letter": "B"}},
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if block.cont == nil {
		t.Fatal("builder did not hand the block a continuation")
	}

	// Run resumed tails inline so the test observes them deterministically.
	var resumed []string
	flow.SetResume(func(ctx context.Context, msg *types.Message, tail core.MessageProcessor) error {
		out, err := tail.Process(ctx, msg)
		if err != nil {
			return err
		}
		body, _ := out.Body.(string)
		resumed = append(resumed, body)
		return nil
	})

	// The block takes over: the chain stops at it, and the two marks run only
	// through the continuation — once per message it chooses to resume.
	out, err := flow.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !out.StopRequested() {
		t.Fatal("the chain should have stopped at the block that took over")
	}
	if len(resumed) != 0 {
		t.Fatalf("nothing should have resumed yet, got %v", resumed)
	}

	for _, seed := range []string{"x", "y"} {
		msg := mustMessage(t)
		msg.Body = seed
		if err := block.cont.Resume(context.Background(), msg); err != nil {
			t.Fatalf("Resume: %v", err)
		}
	}
	if strings.Join(resumed, ",") != "xAB,yAB" {
		t.Fatalf("tail should be the two marks after the block, ran %v", resumed)
	}
}

func TestContinuationRejectsTakeoverAsLastBlock(t *testing.T) {
	reg, _ := continuationRegistry(t)

	_, err := rootBuilder(reg).flow(types.FlowConfig{Process: []types.BlockConfig{
		{Type: "pass"},
		{Type: "takeover"},
	}})
	if err == nil {
		t.Fatal("a block that continues the flow must not be the last in its chain")
	}
	if !strings.Contains(err.Error(), "last block") {
		t.Fatalf("error should name the problem, got %q", err)
	}
}

func TestContinuationNotWiredInsideAComposite(t *testing.T) {
	reg, block := continuationRegistry(t)

	// A sub-flow ends at the composite that owns it, so there is no rest-of-flow
	// to hand down and the block must be left without a continuation.
	then := types.FlowConfig{Process: []types.BlockConfig{{Type: "takeover"}, {Type: "pass"}}}
	_, err := rootBuilder(reg).flow(types.FlowConfig{Process: []types.BlockConfig{
		{Type: blockKindIf, Condition: "true", Then: &then},
		{Type: "pass"},
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if block.cont != nil {
		t.Fatal("a block inside a composite sub-flow must not be given a continuation")
	}
}

func TestContinuationWithoutResumeHookErrors(t *testing.T) {
	reg, block := continuationRegistry(t)

	// A flow built outside the runtime has no resume hook, so the work the block
	// took over cannot be scheduled. Failing is the only honest answer.
	if _, err := rootBuilder(reg).flow(types.FlowConfig{Process: []types.BlockConfig{
		{Type: "takeover"},
		{Type: "pass"},
	}}); err != nil {
		t.Fatalf("build: %v", err)
	}

	err := block.cont.Resume(context.Background(), mustMessage(t))
	if !errors.Is(err, errNoResume) {
		t.Fatalf("want errNoResume, got %v", err)
	}
}

func TestContinuationTailKeepsBlockAddresses(t *testing.T) {
	reg, block := continuationRegistry(t)

	if _, err := rootBuilder(reg).flow(types.FlowConfig{Process: []types.BlockConfig{
		{Type: "takeover"},
		{Type: "mark", Name: "second", Settings: types.Settings{"letter": "A"}},
	}}); err != nil {
		t.Fatalf("build: %v", err)
	}

	// The tail is a view of the same blocks, so a resumed message addresses
	// exactly as it does in place — spies and breakpoints keep working across a
	// takeover.
	tail := block.cont.(*flowContinuation).tail
	if got := tail.Blocks[0].Path; got != "test.second" {
		t.Fatalf("tail block should keep its address, got %q", got)
	}
}
