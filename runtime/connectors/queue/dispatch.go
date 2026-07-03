// This file provides the "queue-dispatch" block: it sends the current message to
// a queue subject to load balance work across replicas. By default it does a
// fire-and-forget Publish and returns the message unchanged — dispatching is the
// common case. With awaitReply it instead does a Request, waiting for one competing
// consumer to handle the message and folding the reply's body and variables back
// in (the cross-replica analogue of a two-way flow-ref). The subject is a CEL
// expression evaluated per message, so a flow can route or shard work dynamically.
package queue

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("queue-dispatch", newDispatch)
}

// dispatchSettings is the queue-dispatch block's typed configuration.
type dispatchSettings struct {
	// Subject is a CEL expression producing the queue subject to send to (required).
	Subject string `json:"subject"`
	// AwaitReply waits for one consumer's reply and folds its body and variables
	// back into the current message (request). When false (the default) the block
	// publishes fire-and-forget and returns immediately, ignoring any reply.
	AwaitReply bool `json:"awaitReply"`
	// Timeout bounds the wait for a reply when AwaitReply is set; it defaults to the
	// queue service's request timeout.
	Timeout duration `json:"timeout"`
}

// dispatch sends the message to a queue subject. The default is a fire-and-forget
// publish; awaitReply waits for a reply and merges it back.
type dispatch struct {
	subject    *expr.Program
	awaitReply bool
	timeout    time.Duration
	env        map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newDispatch(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg dispatchSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Subject) == "" {
		return nil, errors.New("queue-dispatch requires a \"subject\" expression")
	}
	program, err := expr.CompileMessage(deps.Resources, cfg.Subject)
	if err != nil {
		return nil, fmt.Errorf("queue-dispatch: compile subject: %w", err)
	}
	return &dispatch{
		subject:    program,
		awaitReply: cfg.AwaitReply,
		timeout:    time.Duration(cfg.Timeout),
		env:        envActivation(deps.Env),
	}, nil
}

// Process resolves the subject, then sends a fresh sub-message (cloned and
// rekeyed so the sub-invocation correlates on its own EventID, not this flow's
// terminal event) to the queue. The default publishes and returns the message
// unchanged; awaitReply requests a reply and folds its body and variables back in.
func (d *dispatch) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	subject, err := d.subject.EvalString(messageActivation(msg, d.env))
	if err != nil {
		return nil, fmt.Errorf("queue-dispatch subject: %w", err)
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, errors.New("queue-dispatch: subject expression evaluated to empty")
	}

	queues := core.RuntimeServicesFromContext(ctx).Queues()

	sub := msg.Clone()
	if _, err := sub.Rekey(); err != nil {
		return nil, fmt.Errorf("queue-dispatch %q: %w", subject, err)
	}

	if !d.awaitReply {
		if err := queues.Publish(ctx, subject, *sub); err != nil {
			return nil, fmt.Errorf("queue-dispatch publish to %q: %w", subject, err)
		}
		return msg, nil
	}

	var opts []core.RequestOption
	if d.timeout > 0 {
		opts = append(opts, core.WithTimeout(d.timeout))
	}
	reply, err := queues.Request(ctx, subject, *sub, opts...)
	if err != nil {
		return nil, fmt.Errorf("queue-dispatch request to %q: %w", subject, err)
	}
	msg.Body = reply.Body
	for k, v := range reply.Variables {
		msg.Variables.Set(k, v)
	}
	return msg, nil
}

// messageActivation maps a message (and the block's resolved env) onto the
// variables a subject expression can reference.
func messageActivation(msg *types.Message, env map[string]any) map[string]any {
	return map[string]any{
		"body":          msg.Body,
		"vars":          map[string]any(msg.Variables),
		"eventID":       msg.EventID,
		"correlationID": msg.CorrelationID,
		"env":           env,
		"now":           time.Now(),
	}
}

// envActivation materializes a resolved env map into the form CEL expects once at
// build time, so it is shared across every message the block processes.
func envActivation(env map[string]string) map[string]any {
	out := make(map[string]any, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}
