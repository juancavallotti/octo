package noop

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/types"
)

// settingCount names the number of messages a noop source emits before idling.
const settingCount = "count"

// source emits a fixed number of empty messages, then idles until stopped. It is
// a reference MessageSource used for examples and tests.
type source struct {
	out   chan<- *types.Message
	count int
	done  chan struct{}
	wg    sync.WaitGroup
}

// NewSource builds a noop source. The optional "count" setting controls how many
// messages it emits (default 0).
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *Connector) NewSource(cfg types.SourceConfig, out chan<- *types.Message) (core.MessageSource, error) {
	count, err := intSetting(cfg.Settings, settingCount)
	if err != nil {
		return nil, err
	}
	return &source{out: out, count: count, done: make(chan struct{})}, nil
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

// intSetting reads an integer setting, accepting the int and float64 forms a
// YAML decoder may produce. A missing key returns 0.
func intSetting(settings map[string]any, key string) (int, error) {
	value, ok := settings[key]
	if !ok {
		return 0, nil
	}
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		return int(v), nil
	default:
		return 0, fmt.Errorf("setting %q must be an integer, got %T", key, value)
	}
}
