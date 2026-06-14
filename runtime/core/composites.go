package core

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/juancavallotti/eip-go/types"
)

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
	pool     *pool
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
		f.pool.submit(func() {
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

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func (b *builder) scope(cfg types.BlockConfig) (MessageProcessor, error) {
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
func (b *builder) fork(cfg types.BlockConfig) (MessageProcessor, error) {
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
