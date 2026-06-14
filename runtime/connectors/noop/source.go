package noop

import (
	"context"
	"log/slog"
	"sync"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// settings is the noop source's typed configuration.
type settings struct {
	// Count is the number of messages to emit before idling (default 0).
	Count int `json:"count"`
}

// source emits a fixed number of empty messages, then idles until stopped. It is
// a reference MessageSource used for examples and tests.
type source struct {
	out   chan<- *types.Message
	count int
	done  chan struct{}
	wg    sync.WaitGroup
}

// NewSource builds a noop source from its typed settings.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *Connector) NewSource(cfg types.SourceConfig, out chan<- *types.Message) (core.MessageSource, error) {
	var set settings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	return &source{out: out, count: set.Count, done: make(chan struct{})}, nil
}

// Start launches the emit loop on its own goroutine.
func (s *source) Start(ctx context.Context) error {
	s.wg.Add(1)
	go s.run(ctx)
	return nil
}

// Stop signals the emit loop to exit and waits for it, guaranteeing no send
// happens after Stop returns.
func (s *source) Stop(context.Context) error {
	close(s.done)
	s.wg.Wait()
	return nil
}

func (s *source) run(ctx context.Context) {
	defer s.wg.Done()
	for i := 0; i < s.count; i++ {
		msg, err := types.NewMessage("")
		if err != nil {
			slog.Error("noop source failed to build message", "error", err)
			return
		}
		select {
		case s.out <- msg:
		case <-ctx.Done():
			return
		case <-s.done:
			return
		}
	}
}
