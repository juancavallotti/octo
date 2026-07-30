// This file provides the "publish-event" block: it broadcasts the current message
// to a topic subject so every subscriber on that subject receives it. The subject
// is a CEL expression evaluated per message, so a flow can route dynamically; an
// optional value expression replaces the published body (defaulting to the whole
// body). It is fire-and-forget and returns the message unchanged — the broadcast
// counterpart to queue-dispatch's publish.
package events

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerPublish() {
	core.MustRegisterBlock("publish-event", newPublish)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "publish-event",
		Label:    "Publish Event",
		Category: core.CategoryProcessor,
		Description: "Broadcast the current message to a platform topic subject, so every " +
			"subscriber on that subject receives it (fire-and-forget fan-out). The message " +
			"passes through unchanged.",
		Config: reflect.TypeFor[publishSettings](),
	})
}

// publishSettings is the publish-event block's typed configuration.
type publishSettings struct {
	// CEL expression for the topic subject to broadcast to; enables per-message
	// routing.
	Subject string `json:"subject" octo:"label=Subject,type=cel,required"`
	// CEL expression whose result becomes the published body. Empty publishes the
	// whole current body.
	Value string `json:"value" octo:"label=Value,type=cel"`
}

// publish broadcasts the message to a topic subject. It publishes a fresh
// sub-message (cloned and rekeyed so each subscriber's flow correlates on its own
// EventID) and returns the current message unchanged.
type publish struct {
	subject *expr.Program
	value   *expr.Program // nil publishes the current body
	env     map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newPublish(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg publishSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.Subject) == "" {
		return nil, errors.New("publish-event requires a \"subject\" expression")
	}
	subject, err := expr.CompileMessage(deps.Resources, cfg.Subject)
	if err != nil {
		return nil, fmt.Errorf("publish-event: compile subject: %w", err)
	}

	block := &publish{subject: subject, env: expr.EnvActivation(deps.Env)}
	if cfg.Value != "" {
		value, valueErr := expr.CompileMessage(deps.Resources, cfg.Value)
		if valueErr != nil {
			return nil, fmt.Errorf("publish-event: compile value: %w", valueErr)
		}
		block.value = value
	}
	return block, nil
}

// Process resolves the subject, builds a fresh sub-message (cloned, its body
// optionally replaced by the value expression, and rekeyed), and broadcasts it on
// the topics service. The current message passes through unchanged.
func (p *publish) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	activation := expr.MessageActivation(msg, p.env)
	subject, err := p.subject.EvalString(activation)
	if err != nil {
		return nil, fmt.Errorf("publish-event subject: %w", err)
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, errors.New("publish-event: subject expression evaluated to empty")
	}

	sub := msg.Clone()
	if p.value != nil {
		value, evalErr := p.value.Eval(activation)
		if evalErr != nil {
			return nil, fmt.Errorf("publish-event value: %w", evalErr)
		}
		sub.Body = value
	}
	if _, err := sub.Rekey(); err != nil {
		return nil, fmt.Errorf("publish-event %q: %w", subject, err)
	}

	if err := core.RuntimeServicesFromContext(ctx).Topics().Publish(ctx, subject, *sub); err != nil {
		return nil, fmt.Errorf("publish-event publish to %q: %w", subject, err)
	}
	return msg, nil
}
