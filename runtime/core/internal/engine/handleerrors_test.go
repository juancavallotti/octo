package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

// buildHandleErrors builds a handle-errors block from the given config, failing
// the test if construction errors.
func buildHandleErrors(t *testing.T, reg *core.BlockRegistry, cfg types.BlockConfig) *scope {
	t.Helper()
	proc, err := (&builder{reg: reg, pool: pool.New(0, 0)}).handleErrors(cfg)
	if err != nil {
		t.Fatalf("handleErrors: %v", err)
	}
	s, ok := proc.(*scope)
	if !ok {
		t.Fatalf("handleErrors returned %T, want *scope", proc)
	}
	return s
}

// errorVar reads the structured vars.error map a recovery path is given, failing
// if it is absent or the wrong shape.
func errorVar(t *testing.T, msg *types.Message) map[string]any {
	t.Helper()
	raw, ok := msg.Variables["error"]
	if !ok {
		t.Fatal("vars.error was not set")
	}
	m, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("vars.error is %T, want map[string]any", raw)
	}
	return m
}

func TestHandleErrorsExposesStructuredError(t *testing.T) {
	cfg := types.BlockConfig{
		Type:    "handle-errors",
		Name:    "charge",
		Process: []types.BlockConfig{{Type: "fail"}},
		Error:   []types.BlockConfig{{Type: "pass"}},
	}
	proc := buildHandleErrors(t, testRegistry(), cfg)

	out, err := proc.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil {
		t.Fatal("expected recovered message, got nil")
	}

	e := errorVar(t, out)
	if got := e["flow"]; got != "charge" {
		t.Errorf("vars.error.flow = %v, want %q", got, "charge")
	}
	if got := e["block"]; got != "fail" {
		t.Errorf("vars.error.block = %v, want %q", got, "fail")
	}
	msg, _ := e["message"].(string)
	if msg == "" {
		t.Error("vars.error.message is empty")
	}
}

func TestHandleErrorsSkipsErrorPathOnSuccess(t *testing.T) {
	cfg := types.BlockConfig{
		Type:    "handle-errors",
		Name:    "charge",
		Process: []types.BlockConfig{{Type: "pass"}},
		Error:   []types.BlockConfig{{Type: "fail"}},
	}
	proc := buildHandleErrors(t, testRegistry(), cfg)

	out, err := proc.Process(context.Background(), mustMessage(t))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil {
		t.Fatal("expected message, got nil")
	}
	if _, ok := out.Variables["error"]; ok {
		t.Error("vars.error set on the happy path; error chain must not run")
	}
}

func TestHandleErrorsValidation(t *testing.T) {
	reg := testRegistry()
	tests := []struct {
		name  string
		block types.BlockConfig
	}{
		{name: "missing process", block: types.BlockConfig{Type: "handle-errors", Error: []types.BlockConfig{{Type: "pass"}}}},
		{name: "missing error", block: types.BlockConfig{Type: "handle-errors", Process: []types.BlockConfig{{Type: "pass"}}}},
		{
			name: "foreign slot",
			block: types.BlockConfig{
				Type:     "handle-errors",
				Process:  []types.BlockConfig{{Type: "pass"}},
				Error:    []types.BlockConfig{{Type: "pass"}},
				Branches: []types.FlowConfig{{Process: []types.BlockConfig{{Type: "pass"}}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := (&builder{reg: reg, pool: pool.New(0, 0)}).block(tt.block); err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestBlockErrorUnwrapAndLabel(t *testing.T) {
	sentinel := errors.New("boom")
	be := &blockError{label: "charge", err: sentinel}

	if !errors.Is(be, sentinel) {
		t.Error("blockError does not unwrap to its cause")
	}
	if be.Error() != `block "charge": boom` {
		t.Errorf("blockError.Error() = %q, want %q", be.Error(), `block "charge": boom`)
	}

	msg := mustMessage(t)
	SetErrorVariable(msg, "flow-name", be)
	e := errorVar(t, msg)
	if got := e["block"]; got != "charge" {
		t.Errorf("vars.error.block = %v, want %q (recovered via errors.As)", got, "charge")
	}
	if got := e["flow"]; got != "flow-name" {
		t.Errorf("vars.error.flow = %v, want %q", got, "flow-name")
	}
}
