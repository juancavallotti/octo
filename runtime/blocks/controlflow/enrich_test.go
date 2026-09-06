package controlflow

import (
	"context"
	"reflect"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// enrichRegistry extends the shared test registry with a "bodyvar" leaf that, on
// the isolated scope, rewrites the body to a map and sets a scratch variable, so
// tests can observe what the enrich expressions pull back and what stays isolated.
func enrichRegistry() *core.BlockRegistry {
	reg := testRegistry()
	reg.MustRegister("bodyvar", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Body = map[string]any{"tier": "gold"}
			msg.Variables.Set("leaked", true)
			return msg, nil
		}), nil
	})
	return reg
}

// buildEnrich builds an enrich block from its config, failing the test on error.
func buildEnrich(t *testing.T, reg *core.BlockRegistry, cfg types.BlockConfig) *enrichScope {
	t.Helper()
	cfg.Type = blockKindEnrich
	proc, err := tryBuildBlock(reg, core.BlockDeps{}, cfg)
	if err != nil {
		t.Fatalf("build enrich: %v", err)
	}
	e, ok := proc.(*enrichScope)
	if !ok {
		t.Fatalf("build enrich returned %T, want *enrichScope", proc)
	}
	return e
}

// enrichInput returns a message with a known body and a "keep" variable so tests
// can tell isolated scope state apart from what the enrich expressions propagate.
func enrichInput(t *testing.T) *types.Message {
	t.Helper()
	msg := mustMessage(t)
	msg.Body = "orig"
	msg.Variables.Set("keep", true)
	return msg
}

// bodyFlow is the shared enrichment body: a single "bodyvar" leaf.
func bodyFlow() *types.FlowConfig {
	return &types.FlowConfig{Process: []types.BlockConfig{{Type: "bodyvar"}}}
}

func TestEnrichSetBodyFromScopeResult(t *testing.T) {
	reg := enrichRegistry()
	e := buildEnrich(t, reg, types.BlockConfig{
		// setBody reads the enriched clone's body.
		Settings: types.Settings{"body": bodyFlow(), "setBody": `body.tier`},
	})

	msg := enrichInput(t)
	out, err := e.Process(context.Background(), msg)
	if err != nil {
		t.Fatalf("enrich Process: %v", err)
	}
	if out != msg {
		t.Errorf("enrich returned %p, want the input message %p", out, msg)
	}
	if msg.Body != "gold" {
		t.Errorf("body = %v, want gold (from setBody expression)", msg.Body)
	}
	if _, ok := msg.Variables.Bool("leaked"); ok {
		t.Error("scope-only variable leaked to the parent")
	}
	if keep, _ := msg.Variables.Bool("keep"); !keep {
		t.Error("enrich should leave the incoming variables intact")
	}
}

func TestEnrichSetVarsFromScopeResult(t *testing.T) {
	reg := enrichRegistry()
	e := buildEnrich(t, reg, types.BlockConfig{
		Settings: types.Settings{"body": bodyFlow(), "setVars": map[string]string{
			"tier":  `body.tier`,
			"shout": `body.tier + "!"`,
		}}})

	msg := enrichInput(t)
	if _, err := e.Process(context.Background(), msg); err != nil {
		t.Fatalf("enrich Process: %v", err)
	}
	if got, _ := msg.Variables.String("tier"); got != "gold" {
		t.Errorf("vars.tier = %q, want gold", got)
	}
	if got, _ := msg.Variables.String("shout"); got != "gold!" {
		t.Errorf("vars.shout = %q, want gold!", got)
	}
	// setBody was empty, so the body is untouched and scope state stays isolated.
	if msg.Body != "orig" {
		t.Errorf("body = %v, want orig (no setBody expression)", msg.Body)
	}
	if _, ok := msg.Variables.Bool("leaked"); ok {
		t.Error("scope-only variable leaked to the parent")
	}
}

func TestEnrichIsolationWithoutExpressions(t *testing.T) {
	reg := enrichRegistry()
	// No setBody/setVars: the body runs purely for side-effects and nothing from
	// the isolated scope escapes.
	e := buildEnrich(t, reg, types.BlockConfig{Settings: types.Settings{"body": bodyFlow()}})

	msg := enrichInput(t)
	if _, err := e.Process(context.Background(), msg); err != nil {
		t.Fatalf("enrich Process: %v", err)
	}
	if msg.Body != "orig" {
		t.Errorf("body = %v, want orig (nothing propagates)", msg.Body)
	}
	if _, ok := msg.Variables.Bool("leaked"); ok {
		t.Error("scope-only variable leaked to the parent")
	}
}

// The scope gets its isolation from copied variables, not from a copied body —
// so enriching a large message costs no copy of it. Isolation itself is covered
// by the tests above; this pins the cost.
func TestEnrichSharesTheIncomingBody(t *testing.T) {
	var seen any
	reg := testRegistry()
	reg.MustRegister("record-body", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			seen = reflect.ValueOf(msg.Body).Pointer()
			return msg, nil
		}), nil
	})
	body := types.FlowConfig{Process: []types.BlockConfig{{Type: "record-body"}}}
	e := buildEnrich(t, reg, types.BlockConfig{Settings: types.Settings{"body": &body}})

	msg := mustMessage(t)
	msg.Body = map[string]any{"k": "v"}
	want := reflect.ValueOf(msg.Body).Pointer()

	if _, err := e.Process(context.Background(), msg); err != nil {
		t.Fatalf("enrich Process: %v", err)
	}
	if seen != want {
		t.Error("the scope got a copy of the body instead of the body itself")
	}
}

func TestEnrichChildErrorPropagates(t *testing.T) {
	reg := enrichRegistry()
	e := buildEnrich(t, reg, types.BlockConfig{
		Settings: types.Settings{"body": &types.FlowConfig{Process: []types.BlockConfig{{Type: "fail"}}}}})

	out, err := e.Process(context.Background(), enrichInput(t))
	if err == nil {
		t.Fatal("enrich with a failing body returned nil error")
	}
	if out != nil {
		t.Errorf("enrich returned %v on error, want nil", out)
	}
}

func TestEnrichChildDropDropsMessage(t *testing.T) {
	reg := enrichRegistry()
	e := buildEnrich(t, reg, types.BlockConfig{
		Settings: types.Settings{"body": &types.FlowConfig{Process: []types.BlockConfig{{Type: "drop"}}}}})

	out, err := e.Process(context.Background(), enrichInput(t))
	if err != nil {
		t.Fatalf("enrich Process: %v", err)
	}
	if out != nil {
		t.Errorf("enrich returned %v, want nil when the body drops the message", out)
	}
}

func TestEnrichRejectsInvalidExpressions(t *testing.T) {
	reg := enrichRegistry()

	if _, err := tryBuildBlock(reg, core.BlockDeps{}, types.BlockConfig{
		Type:     blockKindEnrich,
		Settings: types.Settings{"body": bodyFlow(), "setBody": `body.`}}); err == nil {
		t.Error("expected an error for an invalid setBody expression")
	}
	if _, err := tryBuildBlock(reg, core.BlockDeps{}, types.BlockConfig{
		Type:     blockKindEnrich,
		Settings: types.Settings{"body": bodyFlow(), "setVars": map[string]string{"x": `body.`}}}); err == nil {
		t.Error("expected an error for an invalid setVars expression")
	}
}

func TestEnrichRequiresBody(t *testing.T) {
	if _, err := tryBuildBlock(enrichRegistry(), core.BlockDeps{}, types.BlockConfig{Type: blockKindEnrich}); err == nil {
		t.Error("expected an error when the enrich block has no body flow")
	}
}
