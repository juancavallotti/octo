package engine

import (
	"context"
	"errors"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// errBreakpointUnwired is returned when a breakpoint block is built without a
// collector to record into. The block is injected by the runtime around an
// addressed block, never authored in YAML, so an unwired one means a config
// declared it by hand.
var errBreakpointUnwired = errors.New(
	"breakpoint is an internal debugging block injected by --break-at and cannot be declared in a flow")

// breakpointBlock is the composite the runtime wraps implicitly around an
// addressed block so `invoke --break-at` can inspect the message there. It runs
// the block it wraps, records the resulting message, and asks the flow to stop —
// so the snapshot is the message as the next block would have received it.
//
// It holds the wrapped block as a one-block sub-flow (the Process slot), which is
// what makes it a composite rather than a leaf: wrapping, rather than sitting
// after the block, means a target that ends the flow itself — a filter block that
// rejects — is still observed, because its stop is seen on the way out of the
// sub-flow instead of skipping the next block.
//
// It is deliberately absent from the schema registry (RegisterBlockMeta): it is a
// debug-only block that must never appear in the editor palette, in `octo schema`,
// or in the docs. Registering it there would also fail the docs-drift check, which
// requires every schema-visible type to be documented on a reference page.
type breakpointBlock struct {
	inner *Flow
	bp    *core.Breakpoint
}

// Process runs the wrapped block, then records the message and halts the flow.
//
// The record/stop order matters: RequestStop writes a reserved variable into the
// message, so recording first keeps that bookkeeping out of the snapshot the user
// sees. The snapshot is a Clone because the live message keeps travelling after
// the stop — an enrich folds its result back into it, a fork's sibling branches
// run on — and the snapshot must stay the message as it was at this block.
//
// A wrapped block that drops the message or fails is reported as never reached:
// there is no message at that point to show.
func (b *breakpointBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	out, err := b.inner.Process(ctx, msg)
	if err != nil {
		return nil, unlabel(err)
	}
	if out == nil {
		return nil, nil
	}

	b.bp.Record(out.Clone())
	out.RequestStop()
	return out, nil
}

// wrapperSettings is what the injector puts in a wrapper's settings: the chain
// holding the one block it wraps and, for a spy, the address its records file
// under. It is decoded strictly, because nothing but the injector writes it.
type wrapperSettings struct {
	Address string              `json:"address"`
	Process []types.BlockConfig `json:"process"`
}

// breakpoint builds the implicit breakpoint composite. The runtime supplies the
// collector through BlockDeps only when --break-at asked for one, so a config that
// declares the block by hand fails to build.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) breakpoint(settings types.Settings) (core.MessageProcessor, error) {
	if b.deps.Breakpoint == nil {
		return nil, errBreakpointUnwired
	}
	var cfg wrapperSettings
	if err := settings.DecodeStrict(&cfg); err != nil {
		return nil, err
	}
	if len(cfg.Process) == 0 {
		return nil, errors.New("breakpoint block requires the block it wraps")
	}

	inner, err := b.subFlow(types.FlowConfig{Process: cfg.Process})
	if err != nil {
		return nil, err
	}
	return &breakpointBlock{inner: inner, bp: b.deps.Breakpoint}, nil
}
