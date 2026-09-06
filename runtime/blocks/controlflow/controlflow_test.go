package controlflow

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// recordRegistry extends the shared test registry with a "record" leaf that
// appends a marker to a shared slice, so tests can observe which sub-flows ran
// and in what order. A block records its "tag" setting verbatim, or the message
// variable named by its "var" setting (used to watch a foreach loop variable).
func recordRegistry(seen *[]any) *core.BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("record", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		tag, hasTag := s.String("tag")
		varName, _ := s.String("var")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			if hasTag {
				*seen = append(*seen, tag)
			} else {
				*seen = append(*seen, msg.Variables[varName])
			}
			return msg, nil
		}), nil
	})
	return reg
}

// tagFlow is a one-block flow that records the given marker when it runs.
func tagFlow(tag string) types.FlowConfig {
	return types.FlowConfig{Process: []types.BlockConfig{
		{Type: "record", Settings: types.Settings{"tag": tag}},
	}}
}

//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func mustBuild(t testing.TB, reg *core.BlockRegistry, cfg types.BlockConfig) core.MessageProcessor {
	t.Helper()
	return buildBlock(t, reg, core.BlockDeps{}, cfg)
}

func TestIfRoutesOnCondition(t *testing.T) {
	tests := []struct {
		name     string
		amount   float64
		withElse bool
		want     string
		passThru bool
	}{
		{name: "true runs then", amount: 200, withElse: true, want: "then"},
		{name: "false runs else", amount: 10, withElse: true, want: "else"},
		{name: "false no else passes through", amount: 10, withElse: false, passThru: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []any
			reg := recordRegistry(&seen)
			then := tagFlow("then")
			cfg := types.BlockConfig{Type: "if", Settings: types.Settings{"condition": "body.amount > 100", "then": &then}}
			if tt.withElse {
				els := tagFlow("else")
				cfg.Settings["else"] = &els
			}
			proc := mustBuild(t, reg, cfg)

			msg := mustMessage(t)
			msg.Body = map[string]any{"amount": tt.amount}
			out, err := proc.Process(context.Background(), msg)
			if err != nil {
				t.Fatalf("Process: %v", err)
			}
			if out != msg {
				t.Errorf("if returned %p, want input %p", out, msg)
			}
			if tt.passThru {
				if len(seen) != 0 {
					t.Errorf("expected no branch to run, saw %v", seen)
				}
				return
			}
			if len(seen) != 1 || seen[0] != tt.want {
				t.Errorf("seen = %v, want [%s]", seen, tt.want)
			}
		})
	}
}

func TestIfNonBoolConditionErrors(t *testing.T) {
	reg := testRegistry()
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:     "if",
		Settings: types.Settings{"condition": "body.amount", "then": &types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"amount": 5}
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Fatal("expected an error for a non-bool condition")
	}
}

func TestIfBuildValidation(t *testing.T) {
	reg := testRegistry()
	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "missing condition", block: types.BlockConfig{Type: "if", Settings: types.Settings{"then": &types.FlowConfig{}}}},
		{name: "missing then", block: types.BlockConfig{Type: "if", Settings: types.Settings{"condition": "true"}}},
		{
			name: "foreign slot",
			block: types.BlockConfig{
				Type:     "if",
				Settings: types.Settings{"condition": "true", "then": &types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}, "branches": []types.FlowConfig{{}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tryBuildBlock(reg, core.BlockDeps{}, tt.block); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestSwitchMatchesFirstCase(t *testing.T) {
	tests := []struct {
		name     string
		kind     string
		def      bool
		want     string
		passThru bool
	}{
		{name: "first case", kind: "a", def: true, want: "a"},
		{name: "second case", kind: "b", def: true, want: "b"},
		{name: "default", kind: "z", def: true, want: "default"},
		{name: "no match no default", kind: "z", def: false, passThru: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []any
			reg := recordRegistry(&seen)
			cfg := types.BlockConfig{Type: "switch",
				Settings: types.Settings{"cases": []map[string]any{
					{"when": `body.kind == "a"`, "process": tagFlow("a").Process},
					{"when": `body.kind == "b"`, "process": tagFlow("b").Process},
				}}}
			if tt.def {
				def := tagFlow("default")
				cfg.Settings["default"] = &def
			}
			proc := mustBuild(t, reg, cfg)

			msg := mustMessage(t)
			msg.Body = map[string]any{"kind": tt.kind}
			if _, err := proc.Process(context.Background(), msg); err != nil {
				t.Fatalf("Process: %v", err)
			}
			if tt.passThru {
				if len(seen) != 0 {
					t.Errorf("expected no case to run, saw %v", seen)
				}
				return
			}
			if len(seen) != 1 || seen[0] != tt.want {
				t.Errorf("seen = %v, want [%s]", seen, tt.want)
			}
		})
	}
}

func TestSwitchBuildValidation(t *testing.T) {
	reg := testRegistry()
	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "no cases", block: types.BlockConfig{Type: "switch"}},
		{name: "case without when", block: types.BlockConfig{Type: "switch", Settings: types.Settings{"cases": []map[string]any{{"process": tagFlow("x").Process}}}}},
		{
			name: "foreign slot",
			block: types.BlockConfig{
				Type:     "switch",
				Settings: types.Settings{"cases": []map[string]any{{"when": "true", "process": tagFlow("x").Process}}, "branches": []types.FlowConfig{{Process: []types.BlockConfig{{Type: "pass"}}}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tryBuildBlock(reg, core.BlockDeps{}, tt.block); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}

func TestForeachIteratesItems(t *testing.T) {
	var seen []any
	reg := recordRegistry(&seen)
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "record", Settings: types.Settings{"var": "n"}},
	}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0, 3.0}}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != msg {
		t.Errorf("foreach returned %p, want input %p", out, msg)
	}
	if len(seen) != 3 || seen[0] != 1.0 || seen[2] != 3.0 {
		t.Errorf("seen = %v, want [1 2 3]", seen)
	}
	// The loop variable must not leak past the foreach.
	if _, ok := msg.Variables["n"]; ok {
		t.Error("loop variable n leaked past the foreach")
	}
}

func TestForeachRestoresPriorLoopVar(t *testing.T) {
	reg := testRegistry()
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"items": "body.nums", "as": "item", "body": &body}})

	msg := mustMessage(t)
	msg.Variables.Set("item", "original")
	msg.Body = map[string]any{"nums": []any{1.0, 2.0}}
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if got, _ := msg.Variables.String("item"); got != "original" {
		t.Errorf("item = %q, want the restored \"original\"", got)
	}
}

func TestForeachNonArrayErrors(t *testing.T) {
	reg := testRegistry()
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"items": "body.nums", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": "not-an-array"}
	if _, err := proc.Process(context.Background(), msg); err == nil {
		t.Fatal("expected an error iterating a non-array")
	}
}

// mapRegistry adds the blocks a map-mode body needs: "to-body" replaces the body
// with the loop variable (the shape a mapping body produces), "drop-odd" drops the
// message for odd elements, and "stop-at" requests stop once it sees its element.
func mapRegistry() *core.BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("to-body", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		varName, _ := s.String("var")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Body = map[string]any{"n": msg.Variables[varName]}
			return msg, nil
		}), nil
	})
	reg.MustRegister("drop-odd", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		varName, _ := s.String("var")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			if n, ok := msg.Variables[varName].(float64); ok && int(n)%2 == 1 {
				return nil, nil
			}
			return msg, nil
		}), nil
	})
	reg.MustRegister("stop-at", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		varName, _ := s.String("var")
		at, _ := s.String("at")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			if n, ok := msg.Variables[varName].(float64); ok && at == strconv.Itoa(int(n)) {
				msg.RequestStop()
			}
			return msg, nil
		}), nil
	})
	return reg
}

// Map mode collects each iteration's resulting body into an array that replaces
// the message body.
func TestForeachMapCollectsBodies(t *testing.T) {
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "to-body", Settings: types.Settings{"var": "n"}},
	}}
	proc := mustBuild(t, mapRegistry(), types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "map", "items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0, 3.0}}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := []any{
		map[string]any{"n": 1.0}, map[string]any{"n": 2.0}, map[string]any{"n": 3.0},
	}
	if !reflect.DeepEqual(out.Body, want) {
		t.Errorf("body = %#v, want %#v", out.Body, want)
	}
	// The loop runs on clones, so the loop variable never touches the message.
	if _, ok := out.Variables["n"]; ok {
		t.Error("loop variable n leaked past the map")
	}
}

// As many elements out as in: an iteration that drops the message contributes a
// null rather than shortening the array or dropping the whole message.
func TestForeachMapDroppedIterationYieldsNull(t *testing.T) {
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "drop-odd", Settings: types.Settings{"var": "n"}},
		{Type: "to-body", Settings: types.Settings{"var": "n"}},
	}}
	proc := mustBuild(t, mapRegistry(), types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "map", "items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0, 3.0}}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := []any{nil, map[string]any{"n": 2.0}, nil}
	if !reflect.DeepEqual(out.Body, want) {
		t.Errorf("body = %#v, want %#v", out.Body, want)
	}
}

// A stop inside the body halts iteration, writes back what was collected, and
// re-raises the stop so the enclosing flow halts too.
func TestForeachMapStopKeepsPartialArray(t *testing.T) {
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "to-body", Settings: types.Settings{"var": "n"}},
		{Type: "stop-at", Settings: types.Settings{"var": "n", "at": "2"}},
	}}
	proc := mustBuild(t, mapRegistry(), types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "map", "items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0, 3.0}}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := []any{map[string]any{"n": 1.0}, map[string]any{"n": 2.0}}
	if !reflect.DeepEqual(out.Body, want) {
		t.Errorf("body = %#v, want %#v (collected up to the stop)", out.Body, want)
	}
	if !out.StopRequested() {
		t.Error("the stop raised inside the body did not bubble out of the map")
	}
}

// Iterations are independent: a variable set by one does not reach the next.
func TestForeachMapIterationsAreIsolated(t *testing.T) {
	var seen []any
	reg := recordRegistry(&seen)
	reg.MustRegister("leak", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Variables.Set("leaked", "yes")
			return msg, nil
		}), nil
	})
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "record", Settings: types.Settings{"var": "leaked"}},
		{Type: "leak"},
	}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "map", "items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0}}
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}
	// Each iteration starts from the original message, so neither sees the leak.
	if len(seen) != 2 || seen[0] != nil || seen[1] != nil {
		t.Errorf("seen = %#v, want [nil nil] — a variable leaked between iterations", seen)
	}
}

// The map loop hands every iteration the same body value rather than a per-element
// copy. Copying it per element is what made map mode quadratic in the collection
// size (#166), because in map mode the body IS the collection being iterated.
//
// Identity is the deterministic form of that assertion: reintroducing a per-element
// copy flips every one of these and fails in microseconds. A wall-clock or
// allocation threshold would be flaky in CI and would still pass a copy that was
// merely linear with a large constant.
func TestForeachMapSharesTheIncomingBody(t *testing.T) {
	var seen []any
	reg := testRegistry()
	reg.MustRegister("record-body", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			seen = append(seen, reflect.ValueOf(msg.Body).Pointer())
			return msg, nil
		}), nil
	})
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "record-body"}}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "map", "items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0, 3.0}}
	// Capture before Process: map mode replaces the body with the collected array.
	want := reflect.ValueOf(msg.Body).Pointer()

	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}

	if len(seen) != 3 {
		t.Fatalf("body ran %d times, want 3", len(seen))
	}
	for i, got := range seen {
		if got != want {
			t.Errorf("iteration %d got a copy of the body, not the body itself", i)
		}
	}
}

// Sharing the body is safe because a body is replace-only. An iteration that
// insists on mutating in place must go through MutableBody, which copies out
// first — so the collection every later iteration reads is still intact.
func TestForeachMapIterationCannotCorruptTheSharedBody(t *testing.T) {
	var seen []any
	reg := recordRegistry(&seen)
	reg.MustRegister("truncate", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.MutableBody().(map[string]any)["nums"] = []any{}
			return msg, nil
		}), nil
	})
	reg.MustRegister("record-len", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			seen = append(seen, len(msg.Body.(map[string]any)["nums"].([]any)))
			return msg, nil
		}), nil
	})
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "record-len"},
		{Type: "truncate"},
	}}
	proc := mustBuild(t, reg, types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "map", "items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0, 3.0}}
	if _, err := proc.Process(context.Background(), msg); err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := []any{3, 3, 3}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("lengths seen = %#v, want %#v — an iteration corrupted the shared body", seen, want)
	}
}

// The default mode is unchanged: the message threads through and passes out.
func TestForeachIterateModeIsDefault(t *testing.T) {
	body := types.FlowConfig{Process: []types.BlockConfig{
		{Type: "to-body", Settings: types.Settings{"var": "n"}},
	}}
	proc := mustBuild(t, mapRegistry(), types.BlockConfig{
		Type:     "foreach",
		Settings: types.Settings{"mode": "iterate", "items": "body.nums", "as": "n", "body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"nums": []any{1.0, 2.0}}
	out, err := proc.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	// The body's last write wins; nothing is collected.
	if want := (map[string]any{"n": 2.0}); !reflect.DeepEqual(out.Body, want) {
		t.Errorf("body = %#v, want %#v", out.Body, want)
	}
}

func TestForeachBuildValidation(t *testing.T) {
	reg := testRegistry()
	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "missing items", block: types.BlockConfig{Type: "foreach", Settings: types.Settings{"body": &types.FlowConfig{}}}},
		{name: "missing body", block: types.BlockConfig{Type: "foreach", Settings: types.Settings{"items": "body.nums"}}},
		{
			name: "foreign slot",
			block: types.BlockConfig{
				Type:     "foreach",
				Settings: types.Settings{"items": "body.nums", "body": &types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}, "then": &types.FlowConfig{}}},
		},
		{
			// A typo must fail the build rather than silently iterate when a
			// transformation was meant.
			name: "unknown mode",
			block: types.BlockConfig{
				Type:     "foreach",
				Settings: types.Settings{"items": "body.nums", "mode": "maap", "body": &types.FlowConfig{Process: []types.BlockConfig{{Type: "pass"}}}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tryBuildBlock(reg, core.BlockDeps{}, tt.block); err == nil {
				t.Errorf("expected an error for %s", tt.name)
			}
		})
	}
}
