package cron

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
	robfig "github.com/robfig/cron/v3"
)

// leaderKeyPrefix namespaces a cron source's leader-election key by connector kind,
// so a cron connector's key cannot collide with another connector type that shares
// the same instance name.
const leaderKeyPrefix = "cron_"

// leaderKey returns the leader-election key for a cron source: the prefix plus the
// configured connector instance name, which is unique within an app, so every
// replica agrees on it and distinct cron connectors elect independently. It falls
// back to the source type for an implicitly-resolved connector that names none.
func leaderKey(cfg types.SourceConfig) string {
	name := cfg.Connector
	if name == "" {
		name = cfg.Type
	}
	return leaderKeyPrefix + name
}

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

	// leaderKey identifies this schedule for leader election; emit fires only while
	// this replica holds it, so a schedule triggers once across all replicas.
	leaderKey string
	lease     core.Leadership

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
		leaderKey:     leaderKey(cfg),
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

// Start begins the schedule on its own goroutines without blocking. It first
// acquires the source's leader-election key from the runtime services on the
// context, so emit can gate ticks on leadership (in the standalone module the
// source is always the leader).
func (s *source) Start(ctx context.Context) error {
	s.ctx = ctx
	lease, err := core.RuntimeServicesFromContext(ctx).LeaderElection().Acquire(ctx, s.leaderKey)
	if err != nil {
		return fmt.Errorf("cron source: acquire leadership for %q: %w", s.leaderKey, err)
	}
	s.lease = lease
	s.cron.Start()
	return nil
}

// Stop halts the schedule and waits for any in-flight tick to finish, so no send
// happens after Stop returns. Closing done first unblocks a tick parked on a full
// channel before the runtime drains the downstream workers. It then stops
// campaigning for leadership, releasing the key for another replica.
func (s *source) Stop(context.Context) error {
	close(s.done)
	<-s.cron.Stop().Done()
	if s.lease != nil {
		_ = s.lease.Close()
	}
	return nil
}

// emit builds one message and sends it, dropping the tick if the runtime is
// shutting down rather than blocking on a full channel. A tick is skipped entirely
// while this replica does not hold leadership, so the schedule fires once across
// the cluster rather than on every replica.
func (s *source) emit() {
	if s.lease != nil && !s.lease.IsLeader() {
		slog.Debug("cron tick skipped: not leader", "key", s.leaderKey)
		return
	}

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
