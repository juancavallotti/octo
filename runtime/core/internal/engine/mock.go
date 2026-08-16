package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// errMockUnwired is returned when a mock block is built without the specs to stand
// in with. The block is injected by the runtime in place of an addressed block,
// never authored in YAML, so an unwired one means a config declared it by hand.
var errMockUnwired = errors.New(
	"mock is an internal debugging block injected by --mocks and cannot be declared in a flow")

// mockBlock stands in for the block the user addressed, so `invoke --mocks` can run
// a flow whose real blocks would call the network, an LLM, or a database.
//
// It is a leaf, not a wrapper: it *replaces* the block rather than wrapping it. That
// is the whole point — a wrapped HTTP call would still fire. The block it replaced is
// gone from the tree, which is also why an unmatched message fails rather than
// falling through: there is nothing left to fall through to.
//
// See debug.go for why it is absent from the schema registry.
type mockBlock struct {
	cases []mockCase
	// def is the outcome for a message no case matched. Nil means there is none, and
	// an unmatched message fails.
	def *mockOutcome
	env map[string]any
}

// mockCase is a compiled case: the condition, and what to do when it holds.
type mockCase struct {
	when    *expr.Program
	outcome mockOutcome
}

// mockOutcome is one of the three things a block can do, prepared at build time.
// body and vars hold the case's canned values already encoded, so each message
// decodes its own copy: a decoded map handed to two concurrent messages would be one
// shared map, and a downstream block mutating the body of one would rewrite the
// mock's answer for the other.
type mockOutcome struct {
	body json.RawMessage
	vars json.RawMessage
	err  string
	drop bool
}

// Process finds the first case whose condition holds and applies its outcome.
func (b *mockBlock) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	for i := range b.cases {
		matched, err := evalCondition(b.cases[i].when, msg, b.env)
		if err != nil {
			return nil, fmt.Errorf("mock case %d: %w", i, err)
		}
		if matched {
			return b.cases[i].outcome.apply(msg)
		}
	}
	if b.def != nil {
		return b.def.apply(msg)
	}
	return nil, fmt.Errorf(
		"no mock case matched, and there is no default: add a `default`, or a case with `when: \"true\"`")
}

// apply turns the outcome into what the block returns.
func (o *mockOutcome) apply(msg *types.Message) (*types.Message, error) {
	switch {
	case o.err != "":
		return nil, errors.New(o.err)
	case o.drop:
		return nil, nil
	}

	// A fresh decode per message, so two messages through the same mock never share
	// the map they carry.
	var body any
	if err := json.Unmarshal(o.body, &body); err != nil {
		return nil, fmt.Errorf("mock body: %w", err)
	}
	msg.SetBody(body)

	if len(o.vars) > 0 {
		var vars map[string]any
		if err := json.Unmarshal(o.vars, &vars); err != nil {
			return nil, fmt.Errorf("mock vars: %w", err)
		}
		for key, value := range vars {
			msg.Variables.Set(key, value)
		}
	}
	return msg, nil
}

// mockSettings is what the injector puts in the block's settings: the address whose
// spec this block stands in for. The spec itself comes from BlockDeps, so there is
// one validated copy of it rather than one parsed from settings per block.
type mockSettings struct {
	Address string `json:"address"`
}

// mock builds the implicit mock leaf. The runtime supplies the specs through
// BlockDeps only when --mocks asked for them, so a config that declares the block by
// hand fails to build.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) mock(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if b.deps.Mocks == nil {
		return nil, errMockUnwired
	}
	// A mock replaces a block, so it holds no sub-flows — not even the ones the block
	// it replaced had.
	if err := allowSlots(cfg, blockKindMock); err != nil {
		return nil, err
	}

	var settings mockSettings
	if err := cfg.Settings.Decode(&settings); err != nil {
		return nil, err
	}
	spec, ok := b.deps.Mocks.For(settings.Address)
	if !ok {
		return nil, fmt.Errorf("mock block has no spec for address %q", settings.Address)
	}

	cases := make([]mockCase, 0, len(spec.Cases))
	for i, c := range spec.Cases {
		// Compiled once, at build time: a mock that cannot compile must fail the run
		// rather than the first message that reaches it.
		when, err := expr.CompileMessage(b.deps.Resources, c.When)
		if err != nil {
			return nil, fmt.Errorf("mock %q case %d: %w", settings.Address, i, err)
		}
		outcome, err := newMockOutcome(c)
		if err != nil {
			return nil, fmt.Errorf("mock %q case %d: %w", settings.Address, i, err)
		}
		cases = append(cases, mockCase{when: when, outcome: outcome})
	}

	block := &mockBlock{cases: cases, env: expr.EnvActivation(b.deps.Env)}
	if spec.Default != nil {
		def, err := newMockOutcome(*spec.Default)
		if err != nil {
			return nil, fmt.Errorf("mock %q default: %w", settings.Address, err)
		}
		block.def = &def
	}
	return block, nil
}

// newMockOutcome encodes a case's canned values once, at build time.
func newMockOutcome(c core.MockCase) (mockOutcome, error) {
	out := mockOutcome{err: c.Error, drop: c.Drop}
	if c.Body == nil {
		return out, nil
	}

	body, err := json.Marshal(c.Body)
	if err != nil {
		return mockOutcome{}, fmt.Errorf("body: %w", err)
	}
	out.body = body

	if len(c.Vars) > 0 {
		vars, varsErr := json.Marshal(c.Vars)
		if varsErr != nil {
			return mockOutcome{}, fmt.Errorf("vars: %w", varsErr)
		}
		out.vars = vars
	}
	return out, nil
}
