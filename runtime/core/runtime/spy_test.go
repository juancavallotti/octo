package runtime

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

func mustSpies(t *testing.T, addresses ...string) *core.Spies {
	t.Helper()
	spies, err := core.NewSpies(addresses)
	if err != nil {
		t.Fatalf("NewSpies: %v", err)
	}
	return spies
}

// TestInjectSpyWrapsTargetWithItsAddress: the wrapper keeps the target's label, so
// errors report the block the user wrote, and carries the address so the engine
// knows which spy to file its records under.
func TestInjectSpyWrapsTargetWithItsAddress(t *testing.T) {
	cfg := sampleConfig()
	const addr = "orders.checkHeader[else].api-call-1"

	if err := injectSpies(cfg, mustSpies(t, addr)); err != nil {
		t.Fatalf("injectSpies: %v", err)
	}

	got := findBlock(t, cfg, addr)
	if got.Type != spyBlockType {
		t.Fatalf("target type = %q, want the spy wrapper", got.Type)
	}
	if got.Name != "api-call-1" {
		t.Errorf("wrapper label = %q, want the target's label", got.Name)
	}
	if address, _ := got.Settings.String("address"); address != addr {
		t.Errorf("wrapper address = %q, want %q", address, addr)
	}
	if len(got.Process) != 1 || blockLabel(got.Process[0]) != "api-call-1" {
		t.Errorf("wrapper must hold the block it wraps, got %+v", got.Process)
	}
}

// TestInjectSpiesDescendsThroughAWrappedComposite is what unwrapDebug exists for.
// Spying a fork replaces its slot with a spy holding the fork, so the next address —
// which descends into one of that fork's branches — would find a spy where the fork
// used to be, and a spy has no branches. Both spies must land.
func TestInjectSpiesDescendsThroughAWrappedComposite(t *testing.T) {
	cfg := sampleConfig()
	const (
		outer = "orders.fanout"               // the fork itself
		inner = "orders.fanout[audit].log-it" // a block inside one of its branches
	)

	if err := injectSpies(cfg, mustSpies(t, outer, inner)); err != nil {
		t.Fatalf("injectSpies: %v", err)
	}

	// The fork is wrapped...
	fork := findBlock(t, cfg, outer)
	if fork.Type != spyBlockType {
		t.Fatalf("the fork is not wrapped: %+v", fork)
	}
	// ...and the address inside it still resolved, through the wrapper.
	logIt := findBlock(t, cfg, inner)
	if logIt.Type != spyBlockType || logIt.Name != "log-it" {
		t.Fatalf("the block inside the wrapped fork is not wrapped: %+v", logIt)
	}
}

// TestApplyDebugNestsBreakpointOutsideSpy pins the injection order. The breakpoint
// halts the flow by writing a reserved variable into the message; a spy wrapped
// around it would record that bookkeeping as part of the block's output, so the
// breakpoint must be the outer of the two.
func TestApplyDebugNestsBreakpointOutsideSpy(t *testing.T) {
	const addr = "orders.first"
	s := &Service{
		config:     *sampleConfig(),
		invokeMode: true,
		spies:      mustSpies(t, addr),
		breakpoint: core.NewBreakpoint(addr),
	}

	if err := s.applyDebug(); err != nil {
		t.Fatalf("applyDebug: %v", err)
	}

	// breakpoint[ spy[ first ] ]
	outer := s.config.Flows[0].Process[0]
	if outer.Type != breakpointBlockType {
		t.Fatalf("outermost block = %q, want the breakpoint", outer.Type)
	}
	if len(outer.Process) != 1 {
		t.Fatalf("the breakpoint holds %d blocks, want 1", len(outer.Process))
	}
	middle := outer.Process[0]
	if middle.Type != spyBlockType {
		t.Fatalf("the block inside the breakpoint = %q, want the spy", middle.Type)
	}
	if len(middle.Process) != 1 || middle.Process[0].Name != "first" {
		t.Fatalf("the spy must hold the target, got %+v", middle.Process)
	}
	// Both wrappers report the block the user wrote.
	if outer.Name != "first" || middle.Name != "first" {
		t.Errorf("wrappers = %q/%q, want both to carry the target's label", outer.Name, middle.Name)
	}
}

// TestInjectSpiesUnknownAddress: a mistyped address is reported against the flag it
// came from, not as a breakpoint.
func TestInjectSpiesUnknownAddress(t *testing.T) {
	err := injectSpies(sampleConfig(), mustSpies(t, "orders.nope"))
	if err == nil {
		t.Fatal("an unknown block must be rejected")
	}
	if !strings.Contains(err.Error(), `spy "orders.nope"`) {
		t.Errorf("error = %q, want it to blame the spy", err)
	}
}

// TestApplyDebugRequiresInvokeMode: a spy is read-only, but nothing drains its
// collector outside an invoke — on a source-backed flow it would hoard the body of
// every message that crossed the block, forever, for nobody.
func TestApplyDebugRequiresInvokeMode(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Service)
	}{
		{name: "spies", apply: func(s *Service) { s.spies = mustSpies(t, "orders.first") }},
		{name: "breakpoint", apply: func(s *Service) { s.breakpoint = core.NewBreakpoint("orders.first") }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{config: *sampleConfig()} // invokeMode is false
			tc.apply(s)

			err := s.applyDebug()
			if err == nil {
				t.Fatal("a debug block on a source-backed service must be refused")
			}
			if !strings.Contains(err.Error(), "invoke mode") {
				t.Errorf("error = %q, want it to say invoke mode is required", err)
			}
		})
	}
}

// TestApplyDebugIsANoopWithoutDebugBlocks: the config a normal run builds from must
// come out of applyDebug untouched.
func TestApplyDebugIsANoopWithoutDebugBlocks(t *testing.T) {
	s := &Service{config: *sampleConfig()}
	if err := s.applyDebug(); err != nil {
		t.Fatalf("applyDebug: %v", err)
	}
	if got := s.config.Flows[0].Process[0]; got.Type != "noop" {
		t.Errorf("a run with no debug blocks was rewritten: %+v", got)
	}
}
