package runtime

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/engine"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// memoryKV is a versioned in-memory store with the standalone module's
// optimistic-concurrency semantics, so the stateful blocks can be exercised
// end to end without pulling in the services module.
type memoryKV struct {
	mu sync.Mutex
	ns map[string]map[string]core.Entry
}

func newMemoryKV() *memoryKV { return &memoryKV{ns: map[string]map[string]core.Entry{}} }

func (s *memoryKV) Get(_ context.Context, namespace, key string) (core.Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.ns[namespace][key]
	if !ok {
		return core.Entry{}, false, nil
	}
	return core.Entry{Value: append([]byte(nil), entry.Value...), Version: entry.Version}, true, nil
}

func (s *memoryKV) Set(
	_ context.Context, namespace, key string, value []byte, expectedVersion int64,
) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys, ok := s.ns[namespace]
	if !ok {
		keys = map[string]core.Entry{}
		s.ns[namespace] = keys
	}
	if keys[key].Version != expectedVersion {
		return 0, core.ErrVersionConflict
	}
	next := expectedVersion + 1
	keys[key] = core.Entry{Value: append([]byte(nil), value...), Version: next}
	return next, nil
}

func (s *memoryKV) Delete(_ context.Context, namespace, key string, expectedVersion int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.ns[namespace][key]
	if !ok {
		return nil
	}
	if expectedVersion != 0 && expectedVersion != entry.Version {
		return core.ErrVersionConflict
	}
	delete(s.ns[namespace], key)
	return nil
}

// memoryServices is the no-op services with a real KV, which is all the
// stateful blocks need. Leadership is granted permanently, matching standalone.
type memoryServices struct {
	core.RuntimeServices
	kv *memoryKV
}

//nolint:ireturn // satisfies the RuntimeServices interface
func (m memoryServices) KV() core.KV { return m.kv }

// collector is a tail block that records every message that reaches it.
type collector struct {
	mu   sync.Mutex
	seen []*types.Message
}

func (c *collector) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, msg)
	return msg, nil
}

func (c *collector) messages() []*types.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*types.Message(nil), c.seen...)
}

// newSplitAggregateFlow binds [split, aggregate, collect] to an inert source,
// with real runtime services on the context.
func newSplitAggregateFlow(t *testing.T, cfg types.FlowConfig) (*boundFlow, context.Context, *collector) {
	t.Helper()
	sink := &collector{}
	reg := testBlocks()
	reg.MustRegister("collect", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return sink, nil
	})

	p := pool.New(0, 0)
	root, err := engine.BuildRoot(cfg, reg, p, nil, core.BlockDeps{})
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	bf := &boundFlow{
		name: "orders", source: fakeSource{}, root: root, workers: 4,
		in: make(chan *types.Message, 16), bus: core.NewEventBus(), pool: p,
	}
	root.SetResume(bf.resume)

	services := memoryServices{RuntimeServices: core.NoopRuntimeServices(), kv: newMemoryKV()}
	return bf, core.ContextWithRuntimeServices(context.Background(), services), sink
}

// waitFor polls until cond holds or the deadline passes.
func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return cond()
}

func TestSplitThenAggregateRoundTrip(t *testing.T) {
	bf, ctx, sink := newSplitAggregateFlow(t, types.FlowConfig{
		Name: "orders",
		Process: []types.BlockConfig{
			{Type: "split", Settings: types.Settings{"items": "body.lines"}},
			{Type: "aggregate"},
			{Type: "collect"},
		},
	})

	if err := bf.start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	msg := mustMessage(t)
	msg.Body = map[string]any{"lines": []any{"a", "b", "c", "d"}}
	bf.in <- msg

	if !waitFor(t, func() bool { return len(sink.messages()) > 0 }) {
		t.Fatal("the aggregated group never reached the tail")
	}
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	// Four elements fan out, each becomes its own invocation, and the aggregator
	// re-joins them into exactly one message that carries on down the flow. This
	// is the whole point of separating the two halves: the elements were processed
	// independently, but the tail sees one group.
	got := sink.messages()
	if len(got) != 1 {
		t.Fatalf("tail saw %d messages, want 1 aggregated group", len(got))
	}
	items, ok := got[0].Body.([]any)
	if !ok || len(items) != 4 {
		t.Fatalf("aggregated body = %#v, want the 4 elements", got[0].Body)
	}
	if count, _ := got[0].Variables.Int("aggregateCount"); count != 4 {
		t.Errorf("aggregateCount = %d, want 4", count)
	}
	if reason, _ := got[0].Variables.String("aggregateReason"); reason != "size" {
		t.Errorf("aggregateReason = %q, want size", reason)
	}
}

func TestSplitThenAggregateKeepsConcurrentMessagesApart(t *testing.T) {
	bf, ctx, sink := newSplitAggregateFlow(t, types.FlowConfig{
		Name: "orders",
		Process: []types.BlockConfig{
			{Type: "split", Settings: types.Settings{"items": "body.lines"}},
			{Type: "aggregate"},
			{Type: "collect"},
		},
	})

	if err := bf.start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Three source messages split concurrently. Each element's groupId is its own
	// message's EventID, so the three groups never mix even though every element
	// runs through the same blocks on the same pool.
	const messages = 3
	for i := range messages {
		msg := mustMessage(t)
		msg.Body = map[string]any{"lines": []any{i, i, i}}
		bf.in <- msg
	}

	if !waitFor(t, func() bool { return len(sink.messages()) == messages }) {
		t.Fatalf("saw %d aggregated groups, want %d", len(sink.messages()), messages)
	}
	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}

	for _, msg := range sink.messages() {
		items, ok := msg.Body.([]any)
		if !ok || len(items) != 3 {
			t.Fatalf("group body = %#v, want 3 elements", msg.Body)
		}
		for _, item := range items {
			if item != items[0] {
				t.Fatalf("group mixes elements from different messages: %#v", items)
			}
		}
	}
}

func TestSplitBackpressureOnASmallPool(t *testing.T) {
	sink := &collector{}
	reg := testBlocks()
	reg.MustRegister("collect", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return sink, nil
	})

	// One worker, one queue slot: far less capacity than the split needs at once.
	// SubmitWait parks the splitting worker instead of failing, so the fan-out
	// runs at the rate the flow drains and every element still arrives.
	p := pool.New(1, 1)
	const elements = 200
	items := make([]any, elements)
	for i := range items {
		items[i] = i
	}

	root, err := engine.BuildRoot(types.FlowConfig{
		Name:    "wide",
		Process: []types.BlockConfig{{Type: "split", Settings: types.Settings{"items": "body.lines"}}, {Type: "collect"}},
	}, reg, p, nil, core.BlockDeps{})
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	bf := &boundFlow{
		name: "wide", source: fakeSource{}, root: root, workers: 1,
		in: make(chan *types.Message, 1), bus: core.NewEventBus(), pool: p,
	}
	root.SetResume(bf.resume)

	if err := bf.start(context.Background()); err != nil {
		t.Fatalf("start: %v", err)
	}
	msg := mustMessage(t)
	msg.Body = map[string]any{"lines": items}
	bf.in <- msg

	if err := bf.stop(context.Background()); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if got := len(sink.messages()); got != elements {
		t.Fatalf("tail saw %d elements, want all %d", got, elements)
	}
}

func TestDuplicateStateKeyIsRejected(t *testing.T) {
	svc := &Service{
		config: types.Config{Flows: []types.FlowConfig{
			{
				Name: "one",
				Process: []types.BlockConfig{
					{Type: "aggregate", Settings: types.Settings{"storeKey": "shared"}}, {Type: "pass"},
				},
			},
			{
				Name: "two",
				Process: []types.BlockConfig{
					{Type: "aggregate", Settings: types.Settings{"storeKey": "shared"}}, {Type: "pass"},
				},
			},
		}},
		blocks:     testBlocks(),
		registry:   core.NewRegistry(),
		invokeMode: true,
	}

	// Two blocks under one state key would fold two unrelated streams into the
	// same groups and emit them twice. The whole config is in view here, so this
	// is where it gets caught.
	_, err := svc.buildFlows(context.Background(), &connectorSet{
		registry: svc.registry, byName: map[string]core.Connector{},
	})
	if err == nil {
		t.Fatal("two blocks sharing a state key must fail the build")
	}
	if !strings.Contains(err.Error(), "storeKey") {
		t.Fatalf("error should suggest the fix, got %q", err)
	}
}
