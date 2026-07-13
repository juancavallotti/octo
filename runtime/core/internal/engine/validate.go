package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// defaultValidateStatus is the response status a validate block rejects with when
// no rejectStatus is configured (422 Unprocessable Entity: the request was
// well-formed but failed the business-rule assertions).
const defaultValidateStatus = 422

// validationErrorsVar holds the messages of the rules that failed, exposed so an
// onReject sub-flow (and the built-in response) can report them.
const validationErrorsVar = "validationErrors"

// validateRule pairs a compiled boolean guard with the message surfaced when it
// does not hold.
type validateRule struct {
	program *expr.Program
	message string
}

// validateBlock is a filter composite: it evaluates a set of boolean rules
// against the message and, if any fail, rejects it — running the onReject
// sub-flow (or writing a built-in response) and requesting the flow stop. When
// every rule holds the message passes through unchanged.
type validateBlock struct {
	rules        []validateRule
	onReject     *Flow // nil uses the built-in default response
	rejectStatus int
	env          map[string]any
}

// Process evaluates every rule against the message, collecting the messages of
// all that fail into vars.validationErrors. If any failed the block rejects (via
// applyReject); otherwise it returns the message unchanged.
func (v *validateBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	var failures []any
	for i := range v.rules {
		ok, err := evalCondition(v.rules[i].program, msg, v.env)
		if err != nil {
			return nil, fmt.Errorf("validate rule %d: %w", i, err)
		}
		if !ok {
			failures = append(failures, v.rules[i].message)
		}
	}
	if len(failures) == 0 {
		return msg, nil
	}

	msg.Variables.Set(validationErrorsVar, failures)
	body := map[string]any{
		"error":    "validation_failed",
		"messages": failures,
	}
	return applyReject(ctx, msg, v.onReject, v.rejectStatus, body)
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) validateBlock(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Rules) == 0 {
		return nil, errors.New("validate block requires at least one rule")
	}
	if err := allowSlots(cfg, blockKindValidate, "rules", "onReject", "rejectStatus"); err != nil {
		return nil, err
	}

	rules := make([]validateRule, 0, len(cfg.Rules))
	for i := range cfg.Rules {
		if cfg.Rules[i].Expr == "" {
			return nil, fmt.Errorf("validate rule %d requires an expr", i)
		}
		program, err := expr.CompileMessage(b.deps.Resources, cfg.Rules[i].Expr)
		if err != nil {
			return nil, fmt.Errorf("validate rule %d: %w", i, err)
		}
		rules = append(rules, validateRule{program: program, message: cfg.Rules[i].Message})
	}

	block := &validateBlock{
		rules:        rules,
		rejectStatus: cfg.RejectStatus,
		env:          expr.EnvActivation(b.deps.Env),
	}
	if block.rejectStatus == 0 {
		block.rejectStatus = defaultValidateStatus
	}
	onReject, err := buildOnReject(b, cfg.OnReject)
	if err != nil {
		return nil, err
	}
	block.onReject = onReject
	return block, nil
}
