package runtime

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// TestBlockPathsResolveToTheBlockThatEmittedThem is the property that makes a
// block event's Path worth carrying: it is not a path-shaped label, it is an
// address — the same string `octo invoke --spies` accepts, resolving back to the
// very block that emitted it.
//
// The two derivations meet here for the first time: the builder mints paths on the
// way down (core/engine), and rewriteTarget walks them back down the
// config. Asserting a hand-written list of strings would only pin the minting; this
// pins the pair, and it fails the day a composite grows a branch slot the builder
// descends through without naming.
func TestBlockPathsResolveToTheBlockThatEmittedThem(t *testing.T) {
	cfg := everyBranchConfig()

	events := core.NewBlockEvents()
	seen := map[string]bool{}
	var order []string
	// A sync listener is called from every goroutine the flow runs on — a fork's
	// branches run on the shared pool — so it takes a lock, as the contract says.
	var mu sync.Mutex
	events.AddSync(func(_ context.Context, ev types.BlockEvent) {
		if ev.Kind != types.BlockPreInvoke {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if seen[ev.Path] {
			return
		}
		seen[ev.Path] = true
		order = append(order, ev.Path)
	})

	svc := NewService(cfg, core.DefaultRegistry(), WithInvokeMode(), WithBlockEvents(events))
	stop := startService(t, svc)
	defer stop()

	// The first flow takes every branch and completes; the second fails on purpose
	// so its flow-level error chain runs.
	for _, flow := range []string{"orders", "failing"} {
		msg, err := types.NewMessage("")
		if err != nil {
			t.Fatalf("NewMessage: %v", err)
		}
		msg.Body = map[string]any{"items": []any{"a"}}
		if _, err = svc.Flows().Call(context.Background(), flow, msg); err != nil {
			t.Logf("call %s: %v", flow, err)
		}
	}

	if len(order) == 0 {
		t.Fatal("the flow emitted no block events")
	}

	sort.Strings(order)
	for _, path := range order {
		t.Run(path, func(t *testing.T) {
			block, resolveErr := resolveTarget(&cfg, "block path", path)
			if resolveErr != nil {
				t.Fatalf("the minted path does not address a block: %v", resolveErr)
			}
			// resolveTarget selects by name, then type, then ref — the same
			// precedence the path was minted with, so the block it lands on must be
			// the one whose label closes the address.
			if got := blockLabel(block); !hasSuffix(path, "."+got) {
				t.Fatalf("path %q resolved to block %q", path, got)
			}
		})
	}

	// Every branch slot the config exercises must have shown up: a slot the builder
	// forgot to name would silently address as its parent's sibling and still
	// resolve, so the shape of the paths is asserted too.
	for _, want := range []string{
		"orders.guard[process].work",
		"orders.guard[error].recover",
		"orders.check[then].on-true",
		"orders.check-else[else].on-false",
		"orders.pick[premium].vip",
		"orders.pick-default[default].standard",
		"orders.fanout[audit].watch",
		"orders.fanout[1].second",
		"orders.lookup[body].fetch",
		"orders.each[body].step",
		"orders.gate[onReject].reject",
		"failing[error].notify",
	} {
		if !seen[want] {
			t.Errorf("no block emitted at %q (saw %v)", want, order)
		}
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

// everyBranchConfig is two flows that between them take every addressable branch
// in a single run: both sides of the if (two blocks, one condition each way), a
// matching case and a default (two switch blocks), a named and an unnamed fork
// branch, an enrich body, a foreach body, and a validate whose rule always fails
// so its onReject runs. The second flow fails on purpose, so its flow-level error
// chain — the one addressed as `<flow>[error]` — runs too.
//
// The blocks are set-variable, which is inert: what is under test is where each
// block sits, not what it does.
func everyBranchConfig() types.Config {
	leaf := func(name string) types.BlockConfig {
		return types.BlockConfig{
			Type: "set-variable", Name: name,
			Settings: types.Settings{"name": name, "value": "true"},
		}
	}
	chain := func(name string) *types.FlowConfig {
		return &types.FlowConfig{Process: []types.BlockConfig{leaf(name)}}
	}
	// failing is the same block with an expression that cannot evaluate, which is
	// how a chain is driven onto its error branch.
	failing := func(name string) types.BlockConfig {
		return types.BlockConfig{
			Type: "set-variable", Name: name,
			Settings: types.Settings{"name": name, "value": "body.missing.nested"},
		}
	}

	return types.Config{
		Flows: []types.FlowConfig{
			{
				Name: "orders",
				Process: []types.BlockConfig{
					{
						// work fails, so both of the block's chains run.
						Type: "handle-errors", Name: "guard",
						Settings: types.Settings{"process": []types.BlockConfig{failing("work")}, "error": []types.BlockConfig{leaf("recover")}}},
					{Type: "if", Name: "check", Settings: types.Settings{"condition": "true", "then": chain("on-true")}},
					{
						Type: "if", Name: "check-else",
						Settings: types.Settings{"condition": "false", "then": chain("never"), "else": chain("on-false")}},
					{
						Type: "switch", Name: "pick",
						Settings: types.Settings{"cases": []map[string]any{{"when": "true", "name": "premium", "process": []types.BlockConfig{leaf("vip")}}}}},
					{
						Type: "switch", Name: "pick-default",
						Settings: types.Settings{"cases": []map[string]any{{"when": "false", "name": "unmatched", "process": []types.BlockConfig{leaf("never")}}}, "default": chain("standard")}},
					{
						Type: "fork", Name: "fanout",
						Settings: types.Settings{"branches": []types.FlowConfig{
							{Name: "audit", Process: []types.BlockConfig{leaf("watch")}},
							{Process: []types.BlockConfig{leaf("second")}},
						}}},
					{Type: "enrich", Name: "lookup", Settings: types.Settings{"body": chain("fetch")}},
					{Type: "foreach", Name: "each", Settings: types.Settings{"items": "body.items", "as": "item", "body": chain("step")}},
					// Last in the chain: a rejected validate stops the flow.
					{
						Type: "validate", Name: "gate",
						Settings: types.Settings{
							"rules":    []map[string]any{{"expr": "false", "message": "always rejects"}},
							"onReject": chain("reject"),
						},
					},
				},
			},
			{
				Name: "failing",
				// A set-variable whose expression cannot evaluate fails the block,
				// which is what puts the flow onto its error chain.
				Process: []types.BlockConfig{{
					Type: "set-variable", Name: "boom",
					Settings: types.Settings{"name": "boom", "value": "body.missing.nested"},
				}},
				Error: []types.BlockConfig{leaf("notify")},
			},
		},
	}
}
