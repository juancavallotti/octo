package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/expr"
	"github.com/juancavallotti/eip-go/core/internal/pool"
	"github.com/juancavallotti/eip-go/types"
)

// defaultForeachVar is the variable name a foreach block binds each element to
// when its config does not name one.
const defaultForeachVar = "item"

// scope is a composite block holding a protected flow and an optional recovery
// flow. Its execution model (transaction/error boundaries) is provisional and
// will be finalized in the processing-model iteration; for now it runs main and,
// on failure, falls back to the alternative flow.
type scope struct {
	main        *Flow
	alternative *Flow
}

// fork is a composite block holding an ordered set of branch flows. It scatters
// the incoming message across its branches and passes the message through
// unchanged; the shared pool runs the branches (concurrently once wired).
type fork struct {
	branches []Flow
	pool     *pool.Pool
}

// Process runs the protected flow, falling back to the alternative on error.
func (s *scope) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	out, err := s.main.Process(ctx, msg)
	if err == nil {
		return out, nil
	}
	if s.alternative == nil {
		return nil, err
	}
	recovered, altErr := s.alternative.Process(ctx, msg)
	if altErr != nil {
		return nil, fmt.Errorf("scope alternative: %w", altErr)
	}
	return recovered, nil
}

// Process scatters the message across its branches, running each on the shared
// pool with its own clone of the message, then joins. The first branch error
// aborts the fork (cancelling the remaining branches); on success the input
// message passes through unchanged. Aggregating branch outputs is deferred.
func (f *fork) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	branchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)

	wg.Add(len(f.branches))
	for i := range f.branches {
		branch := &f.branches[i]
		clone := msg.Clone()
		f.pool.Submit(func() {
			defer wg.Done()
			if _, err := branch.Process(branchCtx, clone); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("fork branch %q: %w", branch.Name, err)
					cancel()
				}
				mu.Unlock()
			}
		})
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	return msg, nil
}

// ifBlock is a composite that runs one of two sub-flows depending on a boolean
// condition evaluated against the message. els is nil when no else flow is
// configured, in which case a false condition passes the message through.
type ifBlock struct {
	condition *expr.Program
	then      *Flow
	els       *Flow
	env       map[string]any
}

// switchCase pairs a compiled boolean guard with the flow to run when it is the
// first case to match.
type switchCase struct {
	when *expr.Program
	flow *Flow
}

// switchBlock is a composite that runs the flow of the first case whose guard is
// true, falling back to def (when set) if none match. A nil def passes the
// message through on no match.
type switchBlock struct {
	cases []switchCase
	def   *Flow
	env   map[string]any
}

// foreachBlock is a composite that runs its body once per element of the array
// produced by items, binding each element to the variable named as. Iteration is
// sequential and the message passes through after the loop.
type foreachBlock struct {
	items *expr.Program
	as    string
	body  *Flow
	env   map[string]any
}

// evalCondition evaluates a boolean expression against the message, erroring if
// the result is not a bool.
func evalCondition(program *expr.Program, msg *types.Message, env map[string]any) (bool, error) {
	value, err := program.Eval(messageActivation(msg, env))
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("condition must evaluate to a bool, got %T", value)
	}
	return result, nil
}

// Process runs the then flow when the condition holds, otherwise the else flow
// (or passes the message through when there is none).
func (i *ifBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	match, err := evalCondition(i.condition, msg, i.env)
	if err != nil {
		return nil, err
	}
	if match {
		return i.then.Process(ctx, msg)
	}
	if i.els != nil {
		return i.els.Process(ctx, msg)
	}
	return msg, nil
}

// Process runs the flow of the first matching case, or the default flow, or
// passes the message through when nothing matches.
func (s *switchBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	for i := range s.cases {
		match, err := evalCondition(s.cases[i].when, msg, s.env)
		if err != nil {
			return nil, fmt.Errorf("switch case %d: %w", i, err)
		}
		if match {
			return s.cases[i].flow.Process(ctx, msg)
		}
	}
	if s.def != nil {
		return s.def.Process(ctx, msg)
	}
	return msg, nil
}

// Process iterates the array produced by items, binding each element to the loop
// variable and running the body in order. A body that drops the message stops
// the loop; an error aborts it. The loop variable is restored to its pre-loop
// state before the message passes through.
func (f *foreachBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	value, err := f.items.Eval(messageActivation(msg, f.env))
	if err != nil {
		return nil, fmt.Errorf("foreach items: %w", err)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("foreach items must evaluate to an array, got %T", value)
	}

	prev, had := msg.Variables[f.as]
	for _, item := range items {
		msg.Variables.Set(f.as, item)
		out, procErr := f.body.Process(ctx, msg)
		if procErr != nil {
			return nil, procErr
		}
		if out == nil {
			return nil, nil
		}
		msg = out
	}

	// Restore the loop variable so it does not leak past the foreach.
	if had {
		msg.Variables.Set(f.as, prev)
	} else {
		delete(msg.Variables, f.as)
	}
	return msg, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) scope(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if cfg.Main == nil {
		return nil, errors.New("scope block requires a main flow")
	}
	if len(cfg.Branches) > 0 {
		return nil, errors.New("scope block must not declare branches")
	}

	main, err := b.subFlow(*cfg.Main)
	if err != nil {
		return nil, fmt.Errorf("scope main: %w", err)
	}

	composite := &scope{main: main}
	if cfg.Alternative != nil {
		alternative, altErr := b.subFlow(*cfg.Alternative)
		if altErr != nil {
			return nil, fmt.Errorf("scope alternative: %w", altErr)
		}
		composite.alternative = alternative
	}
	return composite, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) fork(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Branches) == 0 {
		return nil, errors.New("fork block requires at least one branch")
	}
	if cfg.Main != nil || cfg.Alternative != nil {
		return nil, errors.New("fork block must not declare main/alternative")
	}

	branches := make([]Flow, 0, len(cfg.Branches))
	for i := range cfg.Branches {
		branch, err := b.subFlow(cfg.Branches[i])
		if err != nil {
			return nil, fmt.Errorf("fork branch %d: %w", i, err)
		}
		branches = append(branches, *branch)
	}
	return &fork{branches: branches, pool: b.pool}, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) ifBlock(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if cfg.Condition == "" {
		return nil, errors.New("if block requires a condition")
	}
	if cfg.Then == nil {
		return nil, errors.New("if block requires a then flow")
	}
	if err := allowSlots(cfg, blockKindIf, "condition", "then", "else"); err != nil {
		return nil, err
	}

	condition, err := expr.Compile(cfg.Condition, exprVarNames...)
	if err != nil {
		return nil, err
	}
	then, err := b.subFlow(*cfg.Then)
	if err != nil {
		return nil, fmt.Errorf("if then: %w", err)
	}

	block := &ifBlock{condition: condition, then: then, env: envActivation(b.deps.Env)}
	if cfg.Else != nil {
		els, elseErr := b.subFlow(*cfg.Else)
		if elseErr != nil {
			return nil, fmt.Errorf("if else: %w", elseErr)
		}
		block.els = els
	}
	return block, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) switchBlock(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Cases) == 0 {
		return nil, errors.New("switch block requires at least one case")
	}
	if err := allowSlots(cfg, blockKindSwitch, "cases", "default"); err != nil {
		return nil, err
	}

	cases := make([]switchCase, 0, len(cfg.Cases))
	for i := range cfg.Cases {
		c := cfg.Cases[i]
		if c.When == "" {
			return nil, fmt.Errorf("switch case %d requires a when condition", i)
		}
		when, err := expr.Compile(c.When, exprVarNames...)
		if err != nil {
			return nil, err
		}
		flow, err := b.subFlow(c.Flow)
		if err != nil {
			return nil, fmt.Errorf("switch case %d: %w", i, err)
		}
		cases = append(cases, switchCase{when: when, flow: flow})
	}

	block := &switchBlock{cases: cases, env: envActivation(b.deps.Env)}
	if cfg.Default != nil {
		def, err := b.subFlow(*cfg.Default)
		if err != nil {
			return nil, fmt.Errorf("switch default: %w", err)
		}
		block.def = def
	}
	return block, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) foreachBlock(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if cfg.Items == "" {
		return nil, errors.New("foreach block requires an items expression")
	}
	if cfg.Body == nil {
		return nil, errors.New("foreach block requires a body flow")
	}
	if err := allowSlots(cfg, blockKindForeach, "items", "as", "body"); err != nil {
		return nil, err
	}

	items, err := expr.Compile(cfg.Items, exprVarNames...)
	if err != nil {
		return nil, err
	}
	body, err := b.subFlow(*cfg.Body)
	if err != nil {
		return nil, fmt.Errorf("foreach body: %w", err)
	}

	as := cfg.As
	if as == "" {
		as = defaultForeachVar
	}
	return &foreachBlock{items: items, as: as, body: body, env: envActivation(b.deps.Env)}, nil
}
