package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/types"
)

// sourceSettings configures one topic subscription bound to a flow.
type sourceSettings struct {
	// Topic subject to subscribe to. Every subscriber on the subject (including
	// every replica) receives every message — broadcast fan-out, unlike a queue's
	// competing consumers.
	Subject string `json:"subject" octo:"label=Subject,required"`
	// Number of concurrent handler goroutines; defaults to the topics service default.
	Listeners int `json:"listeners" octo:"label=Listeners"`
}

// source subscribes to a topic subject and turns each broadcast message into a
// flow execution. It is fire-and-forget: a topic has no reply, so the handler just
// forwards the message onto the flow channel and returns, unlike the queue source
// which parks awaiting its flow's result.
type source struct {
	out       chan<- *types.Message
	subject   string
	listeners int

	sub      core.Subscription
	done     chan struct{}
	stopOnce sync.Once
}

// NewSource builds an events source, validating its subject up front. The
// subscription itself is opened in Start, where the runtime services (and so the
// topics backend) are available on the context.
//
//nolint:ireturn // a SourceProvider returns the MessageSource interface
func (c *Connector) NewSource(
	cfg types.SourceConfig, out chan<- *types.Message, _ core.SourceDeps,
) (core.MessageSource, error) {
	var set sourceSettings
	if err := cfg.Settings.Decode(&set); err != nil {
		return nil, err
	}
	if strings.TrimSpace(set.Subject) == "" {
		return nil, errors.New("events source requires a \"subject\" setting")
	}
	return &source{
		out:       out,
		subject:   set.Subject,
		listeners: set.Listeners,
		done:      make(chan struct{}),
	}, nil
}

// Start subscribes to the subject on the core topics service. The handler runs on
// the topics' listener goroutines; Subscribe does not block.
func (s *source) Start(ctx context.Context) error {
	t := core.RuntimeServicesFromContext(ctx).Topics()
	var opts []core.SubscribeOption
	if s.listeners > 0 {
		opts = append(opts, core.WithListeners(s.listeners))
	}
	sub, err := t.Subscribe(ctx, s.subject, s.handle, opts...)
	if err != nil {
		return fmt.Errorf("events source: subscribe to %q: %w", s.subject, err)
	}
	s.sub = sub
	slog.Info("events subscription active", "subject", s.subject)
	return nil
}

// Stop closes the subscription, which cancels the handler context and waits for
// in-flight handlers to drain, so the runtime can safely close the output channel
// afterwards. Closing done first unblocks a handler parked on a full output channel.
func (s *source) Stop(context.Context) error {
	s.stopOnce.Do(func() { close(s.done) })
	if s.sub != nil {
		return s.sub.Close()
	}
	return nil
}

// handle forwards one broadcast message into the flow. It clones and rekeys the
// delivery so each subscriber's flow invocation correlates on its own EventID
// (the publisher and every other subscriber hold copies), preserving the body,
// variables and correlation id.
func (s *source) handle(ctx context.Context, in types.Message) error {
	msg := in.Clone()
	if _, err := msg.Rekey(); err != nil {
		return fmt.Errorf("events source %q: %w", s.subject, err)
	}
	select {
	case s.out <- msg:
	case <-ctx.Done():
	case <-s.done:
	}
	return nil
}
