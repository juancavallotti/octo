package core

import (
	"context"
	"errors"
	"fmt"

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

// fork is a composite block holding an ordered set of branch flows. Its
// execution model (scatter/multicast, output handling) is provisional; for now
// it runs each branch in order with the incoming message and passes the message
// through unchanged.
type fork struct {
	branches []Flow
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

// Process runs each branch with msg and passes msg through unchanged.
func (f *fork) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	for i := range f.branches {
		if _, err := f.branches[i].Process(ctx, msg); err != nil {
			return nil, fmt.Errorf("fork branch %q: %w", f.branches[i].Name, err)
		}
	}
	return msg, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func buildScope(cfg types.BlockConfig, reg *BlockRegistry) (MessageProcessor, error) {
	if cfg.Main == nil {
		return nil, errors.New("scope block requires a main flow")
	}
	if len(cfg.Branches) > 0 {
		return nil, errors.New("scope block must not declare branches")
	}

	main, err := buildSubFlow(*cfg.Main, reg)
	if err != nil {
		return nil, fmt.Errorf("scope main: %w", err)
	}

	composite := &scope{main: main}
	if cfg.Alternative != nil {
		alternative, altErr := buildSubFlow(*cfg.Alternative, reg)
		if altErr != nil {
			return nil, fmt.Errorf("scope alternative: %w", altErr)
		}
		composite.alternative = alternative
	}
	return composite, nil
}

//nolint:ireturn // builders intentionally return the MessageProcessor interface
func buildFork(cfg types.BlockConfig, reg *BlockRegistry) (MessageProcessor, error) {
	if len(cfg.Branches) == 0 {
		return nil, errors.New("fork block requires at least one branch")
	}
	if cfg.Main != nil || cfg.Alternative != nil {
		return nil, errors.New("fork block must not declare main/alternative")
	}

	branches := make([]Flow, 0, len(cfg.Branches))
	for i := range cfg.Branches {
		branch, err := buildSubFlow(cfg.Branches[i], reg)
		if err != nil {
			return nil, fmt.Errorf("fork branch %d: %w", i, err)
		}
		branches = append(branches, *branch)
	}
	return &fork{branches: branches}, nil
}
