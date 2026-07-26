package engine

import (
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// buildRoot builds a root flow with the shared test registry, failing the test on
// a build error.
func buildRoot(t *testing.T, cfg types.FlowConfig, processors ...types.ProcessorConfig) *Flow {
	t.Helper()
	flow, err := BuildRoot(cfg, testRegistry(), pool.New(0, 0), processors, core.BlockDeps{})
	if err != nil {
		t.Fatalf("BuildRoot: %v", err)
	}
	return flow
}

// paths reads the addresses minted for one chain, in order.
func paths(flow *Flow) []string {
	out := make([]string, 0, len(flow.Blocks))
	for i := range flow.Blocks {
		out = append(out, flow.Blocks[i].Path)
	}
	return out
}

// TestBlockPathLabelPrecedence pins the label an address uses: the block as it was
// authored (name, else type, else ref) — not the effective type a ref resolves to.
// A block declared as `{ref: charge-api}` is addressed as `charge-api` even though
// it builds, and reports errors as, a "pass" block.
func TestBlockPathLabelPrecedence(t *testing.T) {
	defs := []types.ProcessorConfig{{Name: "charge-api", Type: "pass"}}

	flow := buildRoot(t, types.FlowConfig{
		Name: "orders",
		Process: []types.BlockConfig{
			{Type: "pass", Name: "charge"},
			{Type: "pass"},
			{Ref: "charge-api"},
			{Type: "pass", Ref: "charge-api"},
		},
	}, defs...)

	want := []string{"orders.charge", "orders.pass", "orders.charge-api", "orders.pass"}
	got := paths(flow)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("block %d path = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBlockPathErrorChain checks the flow's error chain roots at its bracket, so
// its blocks address the way resolveTarget reads them.
func TestBlockPathErrorChain(t *testing.T) {
	cfg := types.FlowConfig{
		Name:    "orders",
		Process: []types.BlockConfig{{Type: "pass", Name: "charge"}},
		Error:   []types.BlockConfig{{Type: "pass", Name: "notify"}},
	}

	errorPath, err := BuildErrorPath(cfg, testRegistry(), pool.New(0, 0), nil, core.BlockDeps{})
	if err != nil {
		t.Fatalf("BuildErrorPath: %v", err)
	}
	if got := paths(errorPath); len(got) != 1 || got[0] != "orders[error].notify" {
		t.Fatalf("error chain paths = %v, want [orders[error].notify]", got)
	}
}

// TestBlockPathAmbiguousChainStillMints documents the one place the minted path
// and the address resolver part ways: two unnamed blocks of the same type in one
// chain both mint the same path, where resolveTarget would refuse the ambiguity.
// The path is an observability label, so minting a shared one beats minting none.
func TestBlockPathAmbiguousChainStillMints(t *testing.T) {
	flow := buildRoot(t, types.FlowConfig{
		Name:    "orders",
		Process: []types.BlockConfig{{Type: "pass"}, {Type: "pass"}},
	})
	if got := paths(flow); got[0] != "orders.pass" || got[1] != "orders.pass" {
		t.Fatalf("paths = %v, want both %q", got, "orders.pass")
	}
}

// TestBlockPathSpyWrapperIsTransparent checks the wrapper the --spies injector adds
// does not show up in the address of the block it wraps, and reports no path of its
// own — so the wrapped block is observed once, at the address the user asked for.
func TestBlockPathSpyWrapperIsTransparent(t *testing.T) {
	spies := mustSpies(t, "orders.charge")

	flow, err := newFlowWithDeps(t, core.BlockDeps{Spies: spies}, types.FlowConfig{
		Name: "orders",
		Process: []types.BlockConfig{
			watch("orders.charge", types.BlockConfig{Type: "pass", Name: "charge"}),
		},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	if got := flow.Blocks[0].Path; got != "" {
		t.Errorf("wrapper path = %q, want empty so it does not report", got)
	}
	spy, ok := flow.Blocks[0].Processor.(*spyBlock)
	if !ok {
		t.Fatalf("block 0 is %T, want *spyBlock", flow.Blocks[0].Processor)
	}
	if got := spy.inner.Blocks[0].Path; got != "orders.charge" {
		t.Errorf("wrapped block path = %q, want %q", got, "orders.charge")
	}
}

// TestBlockPathMockKeepsTargetAddress checks a mock, which stands in its target's
// place rather than wrapping it, reports at the address the target had.
func TestBlockPathMockKeepsTargetAddress(t *testing.T) {
	mocks, err := core.NewMocks(map[string]core.MockSpec{
		"orders.charge": {Default: &core.MockCase{Body: "canned"}},
	})
	if err != nil {
		t.Fatalf("NewMocks: %v", err)
	}

	flow, err := newFlowWithDeps(t, core.BlockDeps{Mocks: mocks}, types.FlowConfig{
		Name:    "orders",
		Process: []types.BlockConfig{stand("orders.charge", "charge")},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := flow.Blocks[0].Path; got != "orders.charge" {
		t.Errorf("mock path = %q, want %q", got, "orders.charge")
	}
}

// newFlowWithDeps builds a root flow with the given block deps.
func newFlowWithDeps(t *testing.T, deps core.BlockDeps, cfg types.FlowConfig) (*Flow, error) {
	t.Helper()
	return BuildRoot(cfg, testRegistry(), pool.New(0, 0), nil, deps)
}
