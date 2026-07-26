package engine

import (
	"context"
	"errors"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// errSpyUnwired is returned when a spy block is built without a collector to record
// into. The block is injected by the runtime around an addressed block, never
// authored in YAML, so an unwired one means a config declared it by hand.
var errSpyUnwired = errors.New(
	"spy is an internal debugging block injected by --spies and cannot be declared in a flow")

// spyBlock is the composite the runtime wraps implicitly around an addressed block
// so `invoke --spies` can watch it. It runs the block it wraps, records what went in
// and what came out, and then gets out of the way: unlike a breakpoint it does not
// stop the flow, so a spied run is the run that would have happened anyway.
//
// It holds the wrapped block as a one-block sub-flow (the Process slot), which is
// what makes it a composite rather than a leaf: wrapping, rather than sitting after
// the block, is what lets it see the outcomes that are not a message — a block that
// drops the message, and a block that fails — because both are visible on the way
// out of the sub-flow.
//
// See debug.go for why it is absent from the schema registry.
type spyBlock struct {
	inner   *Flow
	spies   *core.Spies
	address string
}

// Process runs the wrapped block and records the crossing.
//
// The input is cloned before the block runs, not after: blocks rewrite the fields of
// the message they are given and return the same pointer, so a record taken
// afterwards would show the input already overwritten by the output. The output is
// cloned for the mirror-image reason — the live message travels on, and a later
// block would rewrite a record that pointed at it.
//
// These are full clones rather than scopes: a record outlives the message it was
// taken from and is read by another goroutine, which is the line a scope's shared
// body must not cross.
func (b *spyBlock) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	in := msg.Clone()

	out, err := b.inner.Process(ctx, msg)
	switch {
	case err != nil:
		err = unlabel(err)
		b.record(core.SpyRecord{Input: in, Error: err.Error()})
		return nil, err
	case out == nil:
		b.record(core.SpyRecord{Input: in, Dropped: true})
		return nil, nil
	default:
		b.record(core.SpyRecord{Input: in, Output: out.Clone()})
		return out, nil
	}
}

// record stamps the record with the time it was taken and files it under this spy's
// address.
func (b *spyBlock) record(rec core.SpyRecord) {
	rec.At = time.Now()
	b.spies.Record(b.address, rec)
}

// spySettings is what the injector puts in the wrapper's settings: the address the
// records file under. The block itself is built from the collector on BlockDeps.
type spySettings struct {
	Address string `json:"address"`
}

// spy builds the implicit spy composite. The runtime supplies the collector through
// BlockDeps only when --spies asked for one, so a config that declares the block by
// hand fails to build.
//
//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) spy(cfg types.BlockConfig) (core.MessageProcessor, error) {
	if b.deps.Spies == nil {
		return nil, errSpyUnwired
	}
	if err := allowSlots(cfg, blockKindSpy, "process"); err != nil {
		return nil, err
	}
	if len(cfg.Process) == 0 {
		return nil, errors.New("spy block requires the block it wraps")
	}

	var settings spySettings
	if err := cfg.Settings.Decode(&settings); err != nil {
		return nil, err
	}
	if settings.Address == "" {
		return nil, errors.New("spy block requires the address it records under")
	}

	inner, err := b.subFlow(types.FlowConfig{Process: cfg.Process})
	if err != nil {
		return nil, err
	}
	return &spyBlock{inner: inner, spies: b.deps.Spies, address: settings.Address}, nil
}
