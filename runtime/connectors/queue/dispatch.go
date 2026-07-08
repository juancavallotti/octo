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
	"reflect"
	"strings"
	"time"

	"github.com/juancavallotti/octo/core"
	"github.com/juancavallotti/octo/core/expr"
	"github.com/juancavallotti/octo/types"
)

func init() {
	core.MustRegisterBlock("queue-dispatch", newDispatch)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "queue-dispatch",
		Label:    "Queue Dispatch",
		Category: "processor",
		Group:    "Integration",
		Icon:     "Send",
		Description: "Send the message to a platform queue subject to load balance work across " +
			"replicas. By default it publishes fire-and-forget (one-way); enable Await reply to " +
			"wait for one consumer's reply and fold it back in (request).",
		Config: reflect.TypeFor[dispatchSettings](),
	})
}

// dispatchSettings is the queue-dispatch block's typed configuration.
type dispatchSettings struct {
	// CEL expression for the queue subject to send to; enables per-message
	// routing/sharding.
	Subject string `json:"subject" octo:"label=Subject,type=cel,required"`
	// Wait for one consumer's reply and fold its body and variables back in
	// (request). When false (the default) the message is published fire-and-forget
	// (one-way).
	AwaitReply bool `json:"awaitReply" octo:"label=Await reply,default=false"`
	// How long to wait for a reply when Await reply is set (e.g. 30s); defaults to
	// the queue service timeout.
	Timeout duration `json:"timeout" octo:"label=Timeout,type=string"`
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
		env:        expr.EnvActivation(deps.Env),
	}, nil
}

// Process resolves the subject, then sends a fresh sub-message (cloned and
// rekeyed so the sub-invocation correlates on its own EventID, not this flow's
// terminal event) to the queue. The default publishes and returns the message
// unchanged; awaitReply requests a reply and folds its body and variables back in.
func (d *dispatch) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	subject, err := d.subject.EvalString(expr.MessageActivation(msg, d.env))
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
