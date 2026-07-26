package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/engine"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// fanout is a test block that takes over the flow, resuming the rest of it once
// per element, then stops its own chain — the shape a splitter has.
type fanout struct {
	cont  core.Continuation
	times int
	// started and stopped record the lifecycle calls the runtime makes.
	started, stopped bool
}

func (f *fanout) SetContinuation(c core.Continuation) { f.cont = c }
func (f *fanout) Start(context.Context) error         { f.started = true; return nil }
func (f *fanout) Stop(context.Context) error          { f.stopped = true; return nil }

func (f *fanout) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	for i := 0; i < f.times; i++ {
		elem := msg.Clone()
		if _, err := elem.Rekey(); err != nil {
			return nil, err
		}
		if err := f.cont.Resume(ctx, elem); err != nil {
			return nil, err
		}
	}
	msg.RequestStop()
	return msg, nil
}

// newFanoutFlow binds a [fanout, tail] chain to an inert source, the way the
// service assembles a real flow — including the resume hook.
func newFanoutFlow(t *testing.T, times int, tailType string, rec *recorder) (*boundFlow, *fanout) {
	t.Helper()
	block := &fanout{times: times}

	reg := testBlocks()
	reg.MustRegister("fanout", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return block, nil
	})

	bus := core.NewEventBus()
	bus.Subscribe(rec.handle)
	p := pool.New(0, 0)
	root, err := engine.BuildRoot(
		types.FlowConfig{Process: []types.BlockConfig{{Type: "fanout"}, {Type: tailType}}},
		reg, p, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}

	bf := &boundFlow{
		name:    "test",
		source:  fakeSource{},
		root:    root,
		workers: 1,
		in:      make(chan *types.Message, 8),
		bus:     bus,
		pool:    p,
	}
	root.SetResume(bf.resume)
	return bf, block
}

func TestResumedTailIsItsOwnInvocation(t *testing.T) {
	rec := &recorder{}
	const elements = 3
	bf, _ := newFanoutFlow(t, elements, "pass", rec)

	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	bf.in <- mustMessage(t)
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// One invocation for the message off the source, plus one per resumed
	// element: each tail reports its own started and terminal event, which is what
	// makes per-element progress and failures visible at all.
	kinds := rec.kinds()
	if got, want := countKind(kinds, types.FlowEventStarted), elements+1; got != want {
		t.Errorf("started events = %d, want %d", got, want)
	}
	if got, want := countKind(kinds, types.FlowEventCompleted), elements+1; got != want {
		t.Errorf("completed events = %d, want %d", got, want)
	}
}

func TestResumedTailFailsAlone(t *testing.T) {
	rec := &recorder{}
	const elements = 3
	bf, _ := newFanoutFlow(t, elements, "fail", rec)

	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	bf.in <- mustMessage(t)
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Every element fails in its own invocation and none of them touches the
	// original, which completed the moment it handed the work over. This is the
	// per-element error isolation a fused split-and-join cannot offer: one bad
	// record does not take the others down with it.
	kinds := rec.kinds()
	if got := countKind(kinds, types.FlowEventFailed); got != elements {
		t.Errorf("failed events = %d, want %d", got, elements)
	}
	if got := countKind(kinds, types.FlowEventCompleted); got != 1 {
		t.Errorf("completed events = %d, want 1 (the message that handed off)", got)
	}
}

func TestResumedTailsUseTheErrorPath(t *testing.T) {
	rec := &recorder{}
	const elements = 2
	bf, _ := newFanoutFlow(t, elements, "fail", rec)

	// A resumed tail is a full invocation, so the flow's error chain recovers it
	// exactly as it recovers a message off the source.
	errorPath, err := engine.BuildErrorPath(
		types.FlowConfig{Name: "test", Error: []types.BlockConfig{{Type: "pass"}}},
		testBlocks(), bf.pool, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("BuildErrorPath: %v", err)
	}
	bf.errorPath = errorPath

	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	bf.in <- mustMessage(t)
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	kinds := rec.kinds()
	if got := countKind(kinds, types.FlowEventFailed); got != 0 {
		t.Errorf("failed events = %d, want 0 (the error path recovered them)", got)
	}
	if got, want := countKind(kinds, types.FlowEventCompleted), elements+1; got != want {
		t.Errorf("completed events = %d, want %d", got, want)
	}
}

func TestBoundFlowRunsBlockLifecycle(t *testing.T) {
	rec := &recorder{}
	bf, block := newFanoutFlow(t, 1, "pass", rec)

	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !block.started {
		t.Error("a block owning background work should be started with the flow")
	}
	if block.stopped {
		t.Error("the block should not be stopped while the flow is running")
	}
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if !block.stopped {
		t.Error("the block should be stopped with the flow")
	}
}

// failingLifecycle refuses to start, standing in for a block whose background
// work cannot be set up (an aggregator that cannot reach leader election).
type failingLifecycle struct{ err error }

func (f *failingLifecycle) Start(context.Context) error { return f.err }
func (f *failingLifecycle) Stop(context.Context) error  { return nil }

func (f *failingLifecycle) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	return msg, nil
}

func TestBoundFlowStartFailsWhenABlockCannotStart(t *testing.T) {
	want := errors.New("no leader election")
	reg := testBlocks()
	reg.MustRegister("needy", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return &failingLifecycle{err: want}, nil
	})

	p := pool.New(0, 0)
	root, err := engine.BuildRoot(
		types.FlowConfig{Process: []types.BlockConfig{{Type: "needy"}}},
		reg, p, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	bf := &boundFlow{
		name: "test", source: fakeSource{}, root: root, workers: 1,
		in: make(chan *types.Message, 1), bus: core.NewEventBus(), pool: p,
	}

	// The flow must refuse to come up rather than quietly serve traffic a block
	// cannot handle.
	if err := bf.start(context.Background()); !errors.Is(err, want) {
		t.Fatalf("start error = %v, want it to wrap %v", err, want)
	}
	_ = bf.stop(context.Background())
}

// failingSource refuses to stop, standing in for a source whose teardown errors.
type failingSource struct {
	fakeSource
	err error
}

func (f failingSource) Stop(context.Context) error { return f.err }

func TestBoundFlowShutdownCompletesWhenTheSourceFails(t *testing.T) {
	want := errors.New("source will not stop")
	block := &blockedLifecycle{}
	reg := testBlocks()
	reg.MustRegister("bg", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return block, nil
	})

	p := pool.New(0, 0)
	root, err := engine.BuildRoot(
		types.FlowConfig{Process: []types.BlockConfig{{Type: "bg"}}},
		reg, p, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	bf := &boundFlow{
		name: "test", source: failingSource{err: want}, root: root, workers: 1,
		in: make(chan *types.Message, 1), bus: core.NewEventBus(), pool: p,
	}

	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}

	// The failure is reported, but it is not a reason to abandon the shutdown: the
	// workers would be left parked on a channel nobody closes, a block's own
	// goroutine still running, and the pool never reclaimed.
	if err := bf.stop(context.Background()); !errors.Is(err, want) {
		t.Fatalf("stop error = %v, want it to wrap %v", err, want)
	}

	block.mu.Lock()
	stopped := block.done
	block.mu.Unlock()
	if !stopped {
		t.Error("the block should have been stopped even though the source failed")
	}
	if p.TrySubmit(func() {}) {
		t.Error("the pool should have been torn down even though the source failed")
	}
}

// blockedLifecycle keeps resuming until told to stop, so a test can check the
// pool is still usable while background work is live and that Stop quiets it
// before the pool is torn down.
type blockedLifecycle struct {
	mu   sync.Mutex
	done bool
}

func (b *blockedLifecycle) Start(context.Context) error { return nil }

func (b *blockedLifecycle) Stop(context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.done = true
	return nil
}

func (b *blockedLifecycle) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	return msg, nil
}

func TestBlocksAreStoppedBeforeThePool(t *testing.T) {
	block := &blockedLifecycle{}
	reg := testBlocks()
	reg.MustRegister("bg", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return block, nil
	})

	p := pool.New(0, 0)
	root, err := engine.BuildRoot(
		types.FlowConfig{Process: []types.BlockConfig{{Type: "bg"}}},
		reg, p, nil, core.BlockDeps{},
	)
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	bf := &boundFlow{
		name: "test", source: fakeSource{}, root: root, workers: 1,
		in: make(chan *types.Message, 1), bus: core.NewEventBus(), pool: p,
	}

	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// The pool is stopped last, so a block's own goroutine has already been told
	// to quiet down by the time submitting would be unsafe.
	block.mu.Lock()
	defer block.mu.Unlock()
	if !block.done {
		t.Error("the block should have been stopped before the pool")
	}
}
