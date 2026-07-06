package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/internal/pool"
	"github.com/juancavallotti/octo/types"
)

//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func buildValidate(t *testing.T, cfg types.BlockConfig) core.MessageProcessor {
	t.Helper()
	return buildValidateWithReg(t, testRegistry(), cfg)
}

//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func buildValidateWithReg(t *testing.T, reg *core.BlockRegistry, cfg types.BlockConfig) core.MessageProcessor {
	t.Helper()
	block, err := (&builder{reg: reg, pool: pool.New(0, 0)}).block(cfg)
	if err != nil {
		t.Fatalf("build validate: %v", err)
	}
	return block.Processor
}

func validateMessage(t *testing.T, body any) *types.Message {
	t.Helper()
	msg := mustMessage(t)
	msg.Body = body
	return msg
}

func TestValidatePassesWhenAllRulesHold(t *testing.T) {
	proc := buildValidate(t, types.BlockConfig{
		Type: "validate",
		Rules: []types.RuleConfig{
			{Expr: "body.amount > 0", Message: "amount must be positive"},
			{Expr: "has(body.id)", Message: "id required"},
		},
	})

	out, err := proc.Process(context.Background(), validateMessage(t, map[string]any{"amount": 5.0, "id": "x"}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil {
		t.Fatal("valid message must pass through")
	}
	if out.StopRequested() {
		t.Error("a passing validate must not request stop")
	}
}

func TestValidateBuiltInReject(t *testing.T) {
	proc := buildValidate(t, types.BlockConfig{
		Type: "validate",
		Rules: []types.RuleConfig{
			{Expr: "body.amount > 0", Message: "amount must be positive"},
			{Expr: "has(body.id)", Message: "id required"},
		},
	})

	out, err := proc.Process(context.Background(), validateMessage(t, map[string]any{"amount": -1.0}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("a failing validate must complete and request stop")
	}
	if status, _ := out.Variables.Int(httpStatusVar); status != defaultValidateStatus {
		t.Errorf("status = %d, want %d", status, defaultValidateStatus)
	}
	body, ok := out.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T, want map", out.Body)
	}
	if body["error"] != "validation_failed" {
		t.Errorf("body.error = %v, want validation_failed", body["error"])
	}
	msgs, ok := body["messages"].([]any)
	if !ok || len(msgs) != 2 {
		t.Fatalf("body.messages = %v, want both failing messages", body["messages"])
	}
	if got, _ := out.Variables[validationErrorsVar].([]any); len(got) != 2 {
		t.Errorf("vars.validationErrors = %v, want 2 entries", out.Variables[validationErrorsVar])
	}
}

func TestValidateRejectStatusOverride(t *testing.T) {
	proc := buildValidate(t, types.BlockConfig{
		Type:         "validate",
		RejectStatus: 400,
		Rules:        []types.RuleConfig{{Expr: "false", Message: "always fails"}},
	})

	out, err := proc.Process(context.Background(), validateMessage(t, map[string]any{}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if status, _ := out.Variables.Int(httpStatusVar); status != 400 {
		t.Errorf("status = %d, want 400", status)
	}
}

func TestValidateOnRejectSubflow(t *testing.T) {
	// onReject shapes the response itself; the built-in body must not be applied.
	reg := testRegistry()
	reg.MustRegister("respond-418", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.Body = map[string]any{"custom": true}
			msg.Variables.Set(httpStatusVar, 418)
			return msg, nil
		}), nil
	})
	proc := buildValidateWithReg(t, reg, types.BlockConfig{
		Type:     "validate",
		Rules:    []types.RuleConfig{{Expr: "false", Message: "nope"}},
		OnReject: &types.FlowConfig{Process: []types.BlockConfig{{Type: "respond-418"}}},
	})

	out, err := proc.Process(context.Background(), validateMessage(t, map[string]any{}))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("onReject path must complete and request stop")
	}
	if status, _ := out.Variables.Int(httpStatusVar); status != 418 {
		t.Errorf("status = %d, want 418 from onReject", status)
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["custom"] != true {
		t.Errorf("body = %v, want the onReject custom body", out.Body)
	}
}

func TestValidateRequiresRules(t *testing.T) {
	reg := testRegistry()
	_, err := (&builder{reg: reg, pool: pool.New(0, 0)}).block(types.BlockConfig{Type: "validate"})
	if err == nil {
		t.Fatal("validate without rules must fail to build")
	}
}
