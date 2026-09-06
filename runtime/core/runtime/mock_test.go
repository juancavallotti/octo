package runtime

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// dropSpec is a valid mock spec for the tests that only care about where the mock
// lands, not what it answers.
func dropSpec() core.MockSpec {
	return core.MockSpec{Cases: []core.MockCase{{When: "true", Drop: true}}}
}

func mustMocks(t *testing.T, addresses ...string) *core.Mocks {
	t.Helper()
	specs := make(map[string]core.MockSpec, len(addresses))
	for _, addr := range addresses {
		specs[addr] = dropSpec()
	}
	mocks, err := core.NewMocks(specs)
	if err != nil {
		t.Fatalf("NewMocks: %v", err)
	}
	return mocks
}

// TestInjectMockReplacesTheTarget: unlike a breakpoint or a spy, a mock does not wrap
// the block — it takes its place. The block it replaced is gone, which is what stops
// the real work from happening.
func TestInjectMockReplacesTheTarget(t *testing.T) {
	cfg := sampleConfig()
	const addr = "orders.checkHeader[else].api-call-1"

	if err := injectMocks(cfg, mustMocks(t, addr)); err != nil {
		t.Fatalf("injectMocks: %v", err)
	}

	got := findBlock(t, cfg, addr)
	if got.Type != mockBlockType {
		t.Fatalf("target type = %q, want the mock", got.Type)
	}
	if got.Name != "api-call-1" {
		t.Errorf("mock label = %q, want the target's label so a failure reads like the real block's", got.Name)
	}
	if address, _ := got.Settings.String("address"); address != addr {
		t.Errorf("mock address = %q, want %q", address, addr)
	}
	if inner := wrapped(t, got); len(inner) != 0 {
		t.Errorf("the mock must not keep the block it replaced, got %+v", inner)
	}
}

// TestInjectMockOnACompositeRemovesItsSubtree: mocking a whole fork is a feature —
// it is how you cut out a sub-tree that would call the network. Everything inside it
// must be gone, not merely bypassed.
func TestInjectMockOnACompositeRemovesItsSubtree(t *testing.T) {
	cfg := sampleConfig()

	if err := injectMocks(cfg, mustMocks(t, "orders.fanout")); err != nil {
		t.Fatalf("injectMocks: %v", err)
	}

	fork := findBlock(t, cfg, "orders.fanout")
	if fork.Type != mockBlockType {
		t.Fatalf("the fork was not replaced: %+v", fork)
	}
	if branches, ok := fork.Settings["branches"]; ok {
		t.Errorf("the mocked fork still carries branches: the sub-tree must be gone: %v", branches)
	}
}

// TestApplyDebugNestsMockInsideSpyInsideBreakpoint pins the full injection order. The
// mock must be innermost: injected after a spy it would replace the spy. The
// breakpoint must be outermost: it halts the flow by writing a reserved variable into
// the message, which a spy wrapped around it would record as part of the output.
func TestApplyDebugNestsMockInsideSpyInsideBreakpoint(t *testing.T) {
	const addr = "orders.first"
	s := &Service{
		config:     *sampleConfig(),
		invokeMode: true,
		mocks:      mustMocks(t, addr),
		spies:      mustSpies(t, addr),
		breakpoint: core.NewBreakpoint(addr),
	}

	if err := s.applyDebug(); err != nil {
		t.Fatalf("applyDebug: %v", err)
	}

	// breakpoint[ spy[ mock ] ]
	outer := s.config.Flows[0].Process[0]
	if outer.Type != breakpointBlockType || len(wrapped(t, outer)) != 1 {
		t.Fatalf("outermost block = %+v, want the breakpoint wrapping one block", outer)
	}
	middle := wrapped(t, outer)[0]
	if middle.Type != spyBlockType || len(wrapped(t, middle)) != 1 {
		t.Fatalf("middle block = %+v, want the spy wrapping one block", middle)
	}
	inner := wrapped(t, middle)[0]
	if inner.Type != mockBlockType {
		t.Fatalf("innermost block = %q, want the mock", inner.Type)
	}
	// The spy sees the mock's canned answer, which is the point of combining them.
	if inner.Name != "first" {
		t.Errorf("the mock's label = %q, want the target's", inner.Name)
	}
}

// TestApplyDebugRejectsAnAddressInsideAMock: the mock removed the blocks inside its
// target, so a spy pointed in there can never fire. Reported as the conflict it is,
// rather than as "no such block" — which would read like the user mistyped, when in
// truth they mocked away the thing they were trying to watch.
func TestApplyDebugRejectsAnAddressInsideAMock(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*Service)
		kind  string
	}{
		{
			name:  "spy inside a mocked fork",
			apply: func(s *Service) { s.spies = mustSpies(t, "orders.fanout[audit].log-it") },
			kind:  "spy",
		},
		{
			name:  "breakpoint inside a mocked fork",
			apply: func(s *Service) { s.breakpoint = core.NewBreakpoint("orders.fanout[audit].log-it") },
			kind:  "breakpoint",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{
				config:     *sampleConfig(),
				invokeMode: true,
				mocks:      mustMocks(t, "orders.fanout"),
			}
			tc.apply(s)

			err := s.applyDebug()
			if err == nil {
				t.Fatal("an address inside a mocked block must be rejected")
			}
			if !strings.Contains(err.Error(), "inside mocked block") {
				t.Errorf("error = %q, want it to explain the conflict", err)
			}
			if !strings.Contains(err.Error(), tc.kind) {
				t.Errorf("error = %q, want it to name the %s that is stranded", err, tc.kind)
			}
		})
	}
}

// TestApplyDebugAllowsAnAddressBesideAMock: only blocks *inside* the mocked one are
// stranded. A spy on the block next to it, or on the mock itself, is fine — and
// spying a mocked block is a real use: it shows what the flow fed the block, and what
// the mock answered.
func TestApplyDebugAllowsAnAddressBesideAMock(t *testing.T) {
	s := &Service{
		config:     *sampleConfig(),
		invokeMode: true,
		mocks:      mustMocks(t, "orders.fanout"),
		spies:      mustSpies(t, "orders.first", "orders.fanout"),
	}

	if err := s.applyDebug(); err != nil {
		t.Fatalf("applyDebug: %v", err)
	}
}

// TestApplyDebugMocksRequireInvokeMode: a mock on a source-backed service would
// answer production traffic with a canned response.
func TestApplyDebugMocksRequireInvokeMode(t *testing.T) {
	s := &Service{config: *sampleConfig(), mocks: mustMocks(t, "orders.first")}

	err := s.applyDebug()
	if err == nil {
		t.Fatal("a mock outside invoke mode must be refused")
	}
	if !strings.Contains(err.Error(), "invoke mode") {
		t.Errorf("error = %q, want it to say invoke mode is required", err)
	}
}

// TestInjectMocksUnknownAddress: a mistyped address is reported against the flag it
// came from.
func TestInjectMocksUnknownAddress(t *testing.T) {
	err := injectMocks(sampleConfig(), mustMocks(t, "orders.nope"))
	if err == nil {
		t.Fatal("an unknown block must be rejected")
	}
	if !strings.Contains(err.Error(), `mock "orders.nope"`) {
		t.Errorf("error = %q, want it to blame the mock", err)
	}
}

// TestIsInsideDistinguishesPaths pins the structural check the conflict detection
// rests on: same block name reached through a different branch is a different block.
func TestIsInsideDistinguishesPaths(t *testing.T) {
	tests := []struct {
		name  string
		outer string
		inner string
		want  bool
	}{
		{
			name:  "a block inside the mocked composite",
			outer: "orders.fanout",
			inner: "orders.fanout[audit].log-it",
			want:  true,
		},
		{
			name:  "several levels inside",
			outer: "orders.fanout",
			inner: "orders.fanout[audit].inner[body].deep",
			want:  true,
		},
		{
			name:  "the mocked block itself",
			outer: "orders.fanout",
			inner: "orders.fanout",
			want:  false,
		},
		{
			name:  "a sibling of the mocked block",
			outer: "orders.fanout",
			inner: "orders.first",
			want:  false,
		},
		{
			name:  "the same path in another flow",
			outer: "orders.fanout",
			inner: "other.fanout[audit].log-it",
			want:  false,
		},
		{
			name:  "the same path in the flow's error chain",
			outer: "orders.fanout",
			inner: "orders[error].fanout[audit].log-it",
			want:  false,
		},
		{
			name:  "the same block reached through a different branch",
			outer: "orders.checkHeader[then].charge",
			inner: "orders.checkHeader[else].charge[body].deep",
			want:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			outer, err := resolver{kind: "mock", addr: tc.outer}.parseAddress()
			if err != nil {
				t.Fatalf("parse outer: %v", err)
			}
			inner, err := resolver{kind: "spy", addr: tc.inner}.parseAddress()
			if err != nil {
				t.Fatalf("parse inner: %v", err)
			}
			if got := isInside(outer, inner); got != tc.want {
				t.Errorf("isInside(%q, %q) = %v, want %v", tc.outer, tc.inner, got, tc.want)
			}
		})
	}
}
