package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/juancavallotti/eip-go/core"
	"github.com/juancavallotti/eip-go/core/expr"
	"github.com/juancavallotti/eip-go/types"
	robfig "github.com/robfig/cron/v3"
)

// settings is the cron source's typed configuration.
type settings struct {
	// Schedule is the cron expression that drives the source (required).
	Schedule string `json:"schedule"`
	// Payload is a CEL expression evaluated per tick to build the body.
	Payload string `json:"payload"`
	// CorrelationID is set on every emitted message.
	CorrelationID string `json:"correlationID"`
}

// source emits a message each time its schedule fires. The body comes from the
// compiled payload expression, evaluated with `now` (the fire time) and the
// source's static `settings`.
type source struct {
	out           chan<- *types.Message
	correlationID string
	settings      types.Settings
	payload       *expr.Program

	cron *robfig.Cron
	ctx  context.Context //nolint:containedctx // captured from Start for tick sends
	done chan struct{}
}

// NewSource builds a cron source, parsing the schedule and compiling the payload
// up front so a bad schedule or expression fails at startup.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *Connector) NewSource(cfg types.SourceConfig, out chan<- *types.Message) (core.MessageSource, error) {
	var set settings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	if set.Schedule == "" {
		return nil, fmt.Errorf("cron source requires a %q setting", "schedule")
	}

	s := &source{
		out:           out,
		correlationID: set.CorrelationID,
		settings:      cfg.Settings,
		done:          make(chan struct{}),
	}

	if set.Payload != "" {
		program, compileErr := expr.Compile(set.Payload, "now", "settings")
		if compileErr != nil {
			return nil, compileErr
		}
		s.payload = program
	}

	// Seconds are enabled so schedules have second granularity: a standard
	// expression is six fields (sec min hour dom mon dow), and descriptors like
	// "@every 2s" also work.
	s.cron = robfig.New(robfig.WithSeconds())
	if _, err := s.cron.AddFunc(set.Schedule, s.emit); err != nil {
		return nil, fmt.Errorf("invalid cron schedule %q: %w", set.Schedule, err)
	}
	return s, nil
}

// Start begins the schedule on its own goroutines without blocking.
func (s *source) Start(ctx context.Context) error {
	s.ctx = ctx
	s.cron.Start()
	return nil
}

// Stop halts the schedule and waits for any in-flight tick to finish, so no send
// happens after Stop returns. Closing done first unblocks a tick parked on a full
// channel before the runtime drains the downstream workers.
func (s *source) Stop(context.Context) error {
	close(s.done)
	<-s.cron.Stop().Done()
	return nil
}

// emit builds one message and sends it, dropping the tick if the runtime is
// shutting down rather than blocking on a full channel.
func (s *source) emit() {
	msg, err := types.NewMessage(s.correlationID)
	if err != nil {
		slog.Error("cron source failed to build message", "error", err)
		return
	}
	if err := s.setBody(msg); err != nil {
		slog.Error("cron source failed to build payload", "error", err)
		return
	}

	select {
	case s.out <- msg:
	case <-s.ctx.Done():
	case <-s.done:
	}
}

// setBody evaluates the payload expression and stores the result as the message
// body, normalizing it to decoded-JSON kinds via a round-trip. With no payload
// the body is left empty.
func (s *source) setBody(msg *types.Message) error {
	if s.payload == nil {
		return nil
	}
	value, err := s.payload.Eval(map[string]any{
		"now":      time.Now(),
		"settings": map[string]any(s.settings),
	})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	return msg.SetBodyJSON(raw)
}
