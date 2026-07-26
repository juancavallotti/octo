package engine

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// defaultForeachVar is the variable name a foreach block binds each element to
// when its config does not name one.
const defaultForeachVar = "item"

// The modes a foreach block runs in: iterate (the default) threads the message
// through the body once per element, for its side effects; map collects each
// element's resulting body into an array that replaces the message body.
const (
	foreachModeIterate = "iterate"
	foreachModeMap     = "map"
)

// scope is the internal skeleton for error-handling composites: it runs main and,
// on failure, falls back to the alternative flow. It is not a user-facing block;
// handle-errors builds on it (see builder.handleErrors). The optional onError hook
// lets a specialization act on the message between the failure and the fallback.
type scope struct {
	main        *Flow
	alternative *Flow
	// onError runs against the message just before the alternative flow, after the
	// main flow fails. It is the skeleton's specialization hook: handle-errors sets
	// it to expose the error as vars.error.
	onError func(*types.Message, error)
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
	if s.onError != nil {
		s.onError(msg, err)
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
//
// A branch that requests stop halts the fork's parent chain too: the branches run
// on clones, so the flag would otherwise be discarded with the clone and the stop
// would reach no further than the branch itself. The flag is collected under the
// mutex and applied to the parent message once, after the join — writing it from
// the branch goroutines would race on the message's Variables map.
func (f *fork) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	branchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		stop     bool
	)

	wg.Add(len(f.branches))
	for i := range f.branches {
		branch := &f.branches[i]
		clone := msg.Clone()
		f.pool.Submit(func() {
			defer wg.Done()
			out, err := branch.Process(branchCtx, clone)

			mu.Lock()
			defer mu.Unlock()

			// Read the stop flag before handling the error, and from the clone as
			// well as the result: RequestStop writes the flag into the message it is
			// called on, so a branch that stops and then fails — or whose message is
			// dropped further down the chain — carries the flag on the clone while
			// returning a nil result.
			if clone.StopRequested() || (out != nil && out.StopRequested()) {
				stop = true
			}
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("fork branch %q: %w", branch.Name, err)
				cancel()
			}
		})
	}
	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}
	if stop {
		msg.RequestStop()
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
//
// In map mode the loop is a transformation instead: each element's body runs on
// its own scope and its resulting body becomes one element of an array that
// replaces the message body.
type foreachBlock struct {
	items   *expr.Program
	as      string
	body    *Flow
	mapMode bool
	env     map[string]any
}

// enrichScope is a composite that runs its body flow on an isolated scope of the
// message, then enriches the original message from the scope's result using CEL
// expressions: setBody produces the new body, and each entry in setVars produces
// a variable. The expressions are evaluated against the scope's result, so they
// can reference the enriched body/vars while leaving everything else isolated.
type enrichScope struct {
	body    *Flow
	setBody *expr.Program            // nil leaves the incoming body unchanged
	setVars map[string]*expr.Program // variable name -> expression
	env     map[string]any
}

// Process runs the body on a scope, then applies the enrichment expressions
// against the scope's result. A body error aborts; a body that drops the message
// drops it here too.
//
// The scope shares the incoming body rather than copying it: the body flow runs to
// completion here, on this goroutine, before the message continues, and a body is
// replace-only — so a body flow that sets one rebinds the scope's, leaving the
// message it was called on untouched. Only what setBody/setVars name folds back.
//
// A body that requests stop halts the enclosing flow as well. The body runs on a
// scope and only setBody/setVars fold back, so the flag — which rides in the
// scope's Variables — would otherwise be discarded and the enclosing chain would
// run on. The enrichment expressions still apply, so a stopped body enriches
// exactly as a completed one does.
func (e *enrichScope) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	scoped := msg.Scoped()
	out, err := e.body.Process(ctx, scoped)
	if err != nil {
		return nil, err
	}
	if out == nil {
		return nil, nil
	}

	activation := expr.MessageActivation(out, e.env)
	if e.setBody != nil {
		value, evalErr := e.setBody.Eval(activation)
		if evalErr != nil {
			return nil, fmt.Errorf("enrich setBody: %w", evalErr)
		}
		msg.Body = value
	}
	for name, program := range e.setVars {
		value, evalErr := program.Eval(activation)
		if evalErr != nil {
			return nil, fmt.Errorf("enrich setVars[%q]: %w", name, evalErr)
		}
		msg.Variables.Set(name, value)
	}

	// Carry a stop raised inside the body out to the message the enclosing flow
	// continues with; setVars writes only the names it was configured with, so the
	// flag never crosses back on its own.
	if out.StopRequested() {
		msg.RequestStop()
	}
	return msg, nil
}

// evalCondition evaluates a boolean expression against the message, erroring if
// the result is not a bool.
func evalCondition(program *expr.Program, msg *types.Message, env map[string]any) (bool, error) {
	value, err := program.Eval(expr.MessageActivation(msg, env))
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

// Process iterates the array produced by items, in map mode collecting each
// element's result and otherwise threading the message through the body.
func (f *foreachBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	value, err := f.items.Eval(expr.MessageActivation(msg, f.env))
	if err != nil {
		return nil, fmt.Errorf("foreach items: %w", err)
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("foreach items must evaluate to an array, got %T", value)
	}

	if f.mapMode {
		return f.mapItems(ctx, msg, items)
	}
	return f.iterate(ctx, msg, items)
}

// mapItems runs the body once per element and collects each run's resulting body
// into an array that replaces the message body — the loop as a transformation
// rather than a side effect.
//
// Each element runs on its own scope, so the elements are independent: one
// iteration's variables cannot leak into the next, and the loop variable never
// escapes. The scopes share the incoming body rather than each copying it, which
// is what keeps the loop linear in the size of the collection: in map mode the
// body is usually the collection itself, so a copy per element copied all n
// elements n times. Sharing is safe because a body is replace-only — an iteration
// that sets a body rebinds its own scope's, leaving every later iteration to start
// from the original.
//
// The array is positional — as many elements out as in — so an iteration whose
// body drops the message contributes a null rather than shortening the array or
// dropping the whole message. A stop inside the body halts iteration, writes back
// what was collected, and re-raises the stop on the way out.
func (f *foreachBlock) mapItems(ctx context.Context, msg *types.Message, items []any) (*types.Message, error) {
	mapped := make([]any, 0, len(items))
	stopped := false

	for _, item := range items {
		scoped := msg.Scoped()
		scoped.Variables.Set(f.as, item)

		out, err := f.body.Process(ctx, scoped)
		if err != nil {
			return nil, err
		}
		if out == nil {
			mapped = append(mapped, nil)
			continue
		}

		mapped = append(mapped, out.Body)
		if out.StopRequested() {
			stopped = true
			break
		}
	}

	msg.Body = mapped
	if stopped {
		msg.RequestStop()
	}
	return msg, nil
}

// iterate runs the body once per element, threading the message through each run
// so the body's effects accumulate on it. A body that drops the message drops it
// here too; an error aborts. The loop variable is restored to its pre-loop state
// before the message passes through.
func (f *foreachBlock) iterate(ctx context.Context, msg *types.Message, items []any) (*types.Message, error) {
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
		if msg.StopRequested() {
			// A filter block inside the body requested stop: halt iteration and
			// let the stop bubble up through the enclosing flow loop.
			return msg, nil
		}
	}

	// Restore the loop variable so it does not leak past the foreach.
	if had {
		msg.Variables.Set(f.as, prev)
	} else {
		delete(msg.Variables, f.as)
	}
	return msg, nil
}

// handleErrors builds the error-handling specialization of the scope skeleton: its
// Process chain is the happy path and its Error chain runs on failure, with the
// error exposed to it as vars.error. Both chains are bare block lists (like a
// flow's process), so the block reads as a mini-flow embedded inline.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) handleErrors(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Process) == 0 {
		return nil, errors.New("handle-errors block requires a process chain")
	}
	if len(cfg.Error) == 0 {
		return nil, errors.New("handle-errors block requires an error chain")
	}
	if err := allowSlots(cfg, blockKindHandleErrors, "process", "error"); err != nil {
		return nil, err
	}

	main, err := b.flow(types.FlowConfig{Process: cfg.Process})
	if err != nil {
		return nil, fmt.Errorf("handle-errors process: %w", err)
	}
	alternative, err := b.flow(types.FlowConfig{Process: cfg.Error})
	if err != nil {
		return nil, fmt.Errorf("handle-errors error: %w", err)
	}

	name := cfg.Name
	return &scope{
		main:        main,
		alternative: alternative,
		onError: func(msg *types.Message, e error) {
			SetErrorVariable(msg, name, e)
		},
	}, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) fork(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if len(cfg.Branches) == 0 {
		return nil, errors.New("fork block requires at least one branch")
	}
	if err := allowSlots(cfg, blockKindFork, "branches"); err != nil {
		return nil, err
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

	condition, err := expr.CompileMessage(b.deps.Resources, cfg.Condition)
	if err != nil {
		return nil, err
	}
	then, err := b.subFlow(*cfg.Then)
	if err != nil {
		return nil, fmt.Errorf("if then: %w", err)
	}

	block := &ifBlock{condition: condition, then: then, env: expr.EnvActivation(b.deps.Env)}
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
		when, err := expr.CompileMessage(b.deps.Resources, c.When)
		if err != nil {
			return nil, err
		}
		flow, err := b.subFlow(c.Flow)
		if err != nil {
			return nil, fmt.Errorf("switch case %d: %w", i, err)
		}
		cases = append(cases, switchCase{when: when, flow: flow})
	}

	block := &switchBlock{cases: cases, env: expr.EnvActivation(b.deps.Env)}
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
func (b *builder) enrich(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if cfg.Body == nil {
		return nil, errors.New("enrich block requires a body flow")
	}
	if err := allowSlots(cfg, blockKindEnrich, "body", "setBody", "setVars"); err != nil {
		return nil, err
	}

	body, err := b.subFlow(*cfg.Body)
	if err != nil {
		return nil, fmt.Errorf("enrich body: %w", err)
	}

	block := &enrichScope{body: body, env: expr.EnvActivation(b.deps.Env)}
	if cfg.SetBody != "" {
		setBody, compileErr := expr.CompileMessage(b.deps.Resources, cfg.SetBody)
		if compileErr != nil {
			return nil, fmt.Errorf("enrich setBody: %w", compileErr)
		}
		block.setBody = setBody
	}
	if len(cfg.SetVars) > 0 {
		block.setVars = make(map[string]*expr.Program, len(cfg.SetVars))
		for name, expression := range cfg.SetVars {
			program, compileErr := expr.CompileMessage(b.deps.Resources, expression)
			if compileErr != nil {
				return nil, fmt.Errorf("enrich setVars[%q]: %w", name, compileErr)
			}
			block.setVars[name] = program
		}
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
	if err := allowSlots(cfg, blockKindForeach, "items", "as", "mode", "body"); err != nil {
		return nil, err
	}
	mapMode, err := foreachMapMode(cfg.Mode)
	if err != nil {
		return nil, err
	}

	items, err := expr.CompileMessage(b.deps.Resources, cfg.Items)
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
	return &foreachBlock{
		items:   items,
		as:      as,
		body:    body,
		mapMode: mapMode,
		env:     expr.EnvActivation(b.deps.Env),
	}, nil
}

// foreachMapMode reports whether the configured mode is map, rejecting any mode
// that is neither of the two — a typo must fail the build rather than silently
// iterate when a transformation was meant.
func foreachMapMode(mode string) (bool, error) {
	switch mode {
	case "", foreachModeIterate:
		return false, nil
	case foreachModeMap:
		return true, nil
	default:
		return false, fmt.Errorf("foreach block mode %q must be %q or %q", mode, foreachModeIterate, foreachModeMap)
	}
}
