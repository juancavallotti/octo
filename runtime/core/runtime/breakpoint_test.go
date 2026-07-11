package runtime

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/types"
)

// sub is a one-block sub-flow holding the block named by label.
func sub(labels ...string) *types.FlowConfig {
	flow := types.FlowConfig{Process: blocks(labels...)}
	return &flow
}

// blocks builds a chain of named "noop" blocks.
func blocks(labels ...string) []types.BlockConfig {
	chain := make([]types.BlockConfig, 0, len(labels))
	for _, label := range labels {
		chain = append(chain, types.BlockConfig{Type: "noop", Name: label})
	}
	return chain
}

// sampleConfig is one config exercising every composite an address can descend
// into, so the resolver's branch table is covered end to end by the table below.
func sampleConfig() *types.Config {
	return &types.Config{Flows: []types.FlowConfig{
		{
			Name: "orders",
			Process: []types.BlockConfig{
				{Type: "noop", Name: "first"},
				{Type: "if", Name: "checkHeader", Condition: "true", Then: sub("charge"), Else: sub("api-call-1")},
				{Type: "foreach", Name: "loop", Items: "body.items", Body: sub("transform")},
				{Type: "enrich", Name: "lookup", Body: sub("fetch")},
				{Type: "cache-scope", Name: "cached", Key: `"k"`, Body: sub("compute")},
				{Type: "handle-errors", Name: "guard", Process: blocks("risky"), Error: blocks("recover")},
				{
					Type: "fork", Name: "fanout",
					Branches: []types.FlowConfig{
						{Name: "audit", Process: blocks("log-it")},
						{Process: blocks("unnamed-branch-block")},
					},
				},
				{
					Type: "switch", Name: "pick",
					Cases: []types.CaseConfig{
						{When: "true", Flow: types.FlowConfig{Name: "vip", Process: blocks("comp")}},
						{When: "false", Flow: types.FlowConfig{Process: blocks("plain")}},
					},
					Default: sub("fallback"),
				},
				{
					Type: "ai-router", Name: "route", Connector: "claude", Prompt: "p",
					Routes:  []types.RouteConfig{{Name: "premium", Description: "d", Process: blocks("charge-vip")}},
					Default: sub("guardrail"),
				},
				{
					Type: "ai-agent", Name: "agent", Connector: "claude", Prompt: "p",
					Tools: []types.ToolConfig{{Name: "lookup", Description: "d", Process: blocks("do-lookup")}},
				},
				{Type: "validate", Name: "check", Rules: []types.RuleConfig{{Expr: "true"}}, OnReject: sub("explain")},
				// A block that takes its type from a named processor. It has neither a
				// name nor a type of its own, so its ref is the only handle on it.
				{Ref: "shared-http"},
			},
			Error: blocks("notify"),
		},
		{Name: "other", Process: blocks("step")},
	}}
}

// findBlock walks the same path the injector does and returns the block there, so a
// test can assert what injection produced.
func findBlock(t *testing.T, cfg *types.Config, addr string) types.BlockConfig {
	t.Helper()
	parsed, err := parseAddress(addr)
	if err != nil {
		t.Fatalf("parse %q: %v", addr, err)
	}
	flow, err := findFlow(cfg, parsed.flow)
	if err != nil {
		t.Fatalf("find flow: %v", err)
	}
	chain, err := flowChain(flow, parsed.chain)
	if err != nil {
		t.Fatalf("flow chain: %v", err)
	}
	for _, hop := range parsed.steps[:len(parsed.steps)-1] {
		block, blockErr := selectBlock(*chain, hop.block, addr)
		if blockErr != nil {
			t.Fatalf("select %q: %v", hop.block, blockErr)
		}
		if chain, err = branchChain(block, hop.branch); err != nil {
			t.Fatalf("branch %q: %v", hop.branch, err)
		}
	}
	target, err := selectBlock(*chain, parsed.steps[len(parsed.steps)-1].block, addr)
	if err != nil {
		t.Fatalf("select target: %v", err)
	}
	return *target
}

// TestInjectBreakpointReachesEveryComposite walks an address into each composite the
// grammar can descend through and asserts the target came out wrapped in a
// breakpoint block that still carries the target's label.
func TestInjectBreakpointReachesEveryComposite(t *testing.T) {
	tests := []struct {
		name    string
		addr    string
		wrapped string // the label the wrapper must take, and the type inside it
	}{
		{name: "root chain", addr: "orders.first", wrapped: "first"},
		{name: "if then", addr: "orders.checkHeader[then].charge", wrapped: "charge"},
		{name: "if else", addr: "orders.checkHeader[else].api-call-1", wrapped: "api-call-1"},
		{name: "foreach body", addr: "orders.loop[body].transform", wrapped: "transform"},
		{name: "enrich body", addr: "orders.lookup[body].fetch", wrapped: "fetch"},
		{name: "cache-scope body", addr: "orders.cached[body].compute", wrapped: "compute"},
		{name: "handle-errors process", addr: "orders.guard[process].risky", wrapped: "risky"},
		{name: "handle-errors error", addr: "orders.guard[error].recover", wrapped: "recover"},
		{name: "fork branch by name", addr: "orders.fanout[audit].log-it", wrapped: "log-it"},
		{name: "fork branch by index", addr: "orders.fanout[1].unnamed-branch-block", wrapped: "unnamed-branch-block"},
		{name: "switch case by name", addr: "orders.pick[vip].comp", wrapped: "comp"},
		{name: "switch case by index", addr: "orders.pick[1].plain", wrapped: "plain"},
		{name: "switch default", addr: "orders.pick[default].fallback", wrapped: "fallback"},
		{name: "ai-router route", addr: "orders.route[premium].charge-vip", wrapped: "charge-vip"},
		{name: "ai-router default", addr: "orders.route[default].guardrail", wrapped: "guardrail"},
		{name: "ai-agent tool", addr: "orders.agent[lookup].do-lookup", wrapped: "do-lookup"},
		{name: "filter onReject", addr: "orders.check[onReject].explain", wrapped: "explain"},
		{name: "flow error chain", addr: "orders[error].notify", wrapped: "notify"},
		{name: "another flow", addr: "other.step", wrapped: "step"},
		{name: "block by type", addr: "orders.foreach[body].transform", wrapped: "transform"},
		{name: "block by ref", addr: "orders.shared-http", wrapped: "shared-http"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sampleConfig()
			if err := injectBreakpoint(cfg, tc.addr); err != nil {
				t.Fatalf("injectBreakpoint(%q): %v", tc.addr, err)
			}

			got := findBlock(t, cfg, tc.addr)
			if got.Type != breakpointBlockType {
				t.Fatalf("target type = %q, want the breakpoint wrapper", got.Type)
			}
			// The wrapper carries the target's label so errors report the same block
			// they would have without the breakpoint.
			if got.Name != tc.wrapped {
				t.Errorf("wrapper label = %q, want %q", got.Name, tc.wrapped)
			}
			if len(got.Process) != 1 {
				t.Fatalf("wrapper holds %d blocks, want the single block it wraps", len(got.Process))
			}
			if inner := blockLabel(got.Process[0]); inner != tc.wrapped {
				t.Errorf("wrapped block = %q, want %q", inner, tc.wrapped)
			}
		})
	}
}

// TestInjectBreakpointDeepNesting descends three composites to reach the target,
// which is the shape a real debugging session hits.
func TestInjectBreakpointDeepNesting(t *testing.T) {
	cfg := &types.Config{Flows: []types.FlowConfig{{
		Name: "deep",
		Process: []types.BlockConfig{{
			Type: "if", Name: "outer", Condition: "false",
			Then: sub("skipped"),
			Else: &types.FlowConfig{Process: []types.BlockConfig{{
				Type: "foreach", Name: "loop", Items: "body.items",
				Body: &types.FlowConfig{Process: []types.BlockConfig{{
					Type: "fork", Name: "fanout",
					Branches: []types.FlowConfig{{Name: "audit", Process: blocks("target")}},
				}}},
			}}},
		}},
	}}}

	const addr = "deep.outer[else].loop[body].fanout[audit].target"
	if err := injectBreakpoint(cfg, addr); err != nil {
		t.Fatalf("injectBreakpoint(%q): %v", addr, err)
	}
	got := findBlock(t, cfg, addr)
	if got.Type != breakpointBlockType || got.Name != "target" {
		t.Errorf("deep target = %+v, want a breakpoint wrapping %q", got, "target")
	}
}

// TestInjectBreakpointWrapsOnlyTheTarget: injection must not disturb the rest of
// the tree.
func TestInjectBreakpointWrapsOnlyTheTarget(t *testing.T) {
	cfg := sampleConfig()
	if err := injectBreakpoint(cfg, "orders.checkHeader[else].api-call-1"); err != nil {
		t.Fatalf("injectBreakpoint: %v", err)
	}

	orders := cfg.Flows[0]
	if orders.Process[0].Type != "noop" {
		t.Errorf("the block before the target was rewritten: %+v", orders.Process[0])
	}
	ifBlock := orders.Process[1]
	if ifBlock.Type != "if" || ifBlock.Name != "checkHeader" {
		t.Errorf("the composite on the path was rewritten: %+v", ifBlock)
	}
	if then := ifBlock.Then.Process[0]; then.Type != "noop" || then.Name != "charge" {
		t.Errorf("the branch not addressed was rewritten: %+v", then)
	}
	if cfg.Flows[1].Process[0].Type != "noop" {
		t.Errorf("another flow was rewritten: %+v", cfg.Flows[1].Process[0])
	}
}

func TestInjectBreakpointErrors(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want string
	}{
		{name: "empty", addr: "", want: "empty"},
		{name: "flow only", addr: "orders", want: "at least one block"},
		{name: "unknown flow", addr: "nope.first", want: `no flow named "nope"`},
		{name: "unknown block", addr: "orders.nope", want: `no block "nope"`},
		{name: "unknown branch", addr: "orders.checkHeader[nope].charge", want: `has no branch "nope"`},
		{name: "unknown flow chain", addr: "orders[nope].notify", want: `has no chain "nope"`},
		{name: "leaf has no branch", addr: "orders.first[then].x", want: "this is a leaf block"},
		{name: "branch on the target", addr: "orders.checkHeader[then]", want: "must not name a branch"},
		{name: "missing branch mid-path", addr: "orders.checkHeader.charge", want: "needs a branch"},
		{name: "unclosed bracket", addr: "orders.checkHeader[then.charge", want: "closing bracket"},
		{name: "empty bracket", addr: "orders.checkHeader[].charge", want: "empty bracket"},
		{name: "nested brackets", addr: "orders.fanout[branches[0]].log-it", want: "nested brackets"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := injectBreakpoint(sampleConfig(), tc.addr)
			if err == nil {
				t.Fatalf("injectBreakpoint(%q) succeeded, want an error", tc.addr)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestInjectBreakpointAmbiguousSelector: two unnamed blocks of the same type in one
// chain cannot be told apart, and guessing would break silently at a breakpoint —
// so the address is rejected with advice.
func TestInjectBreakpointAmbiguousSelector(t *testing.T) {
	cfg := &types.Config{Flows: []types.FlowConfig{{
		Name:    "orders",
		Process: []types.BlockConfig{{Type: "rest"}, {Type: "rest"}},
	}}}

	err := injectBreakpoint(cfg, "orders.rest")
	if err == nil {
		t.Fatal("an ambiguous selector must be rejected")
	}
	if !strings.Contains(err.Error(), "matches 2 blocks") {
		t.Errorf("error = %q, want it to report the ambiguity", err)
	}
	if !strings.Contains(err.Error(), "unique name") {
		t.Errorf("error = %q, want it to say how to fix the address", err)
	}
}

// TestInjectBreakpointNameBeatsType: a block is selected by name first, so a block
// named after another's type still resolves to the one the user meant.
func TestInjectBreakpointNameBeatsType(t *testing.T) {
	cfg := &types.Config{Flows: []types.FlowConfig{{
		Name: "orders",
		Process: []types.BlockConfig{
			{Type: "log", Name: "rest"}, // named "rest"
			{Type: "rest"},              // typed "rest"
		},
	}}}

	if err := injectBreakpoint(cfg, "orders.rest"); err != nil {
		t.Fatalf("injectBreakpoint: %v", err)
	}
	if got := cfg.Flows[0].Process[0]; got.Type != breakpointBlockType {
		t.Errorf("the block NAMED rest should have been wrapped, got %+v", got)
	}
	if got := cfg.Flows[0].Process[1]; got.Type != "rest" {
		t.Errorf("the block TYPED rest should have been left alone, got %+v", got)
	}
}
