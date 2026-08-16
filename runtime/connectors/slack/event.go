// This file provides the "slack-event" block: it unwraps a verified Slack
// event_callback into a friendly, flat shape and filters it. Events whose type
// is not in the eventTypes allowlist, or that fail the optional filter CEL
// predicate, are dropped so the flow only handles what it cares about. Signature
// verification is a separate concern handled upstream by slack-verify-request.
package slack

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerEvent() {
	core.MustRegisterBlock("slack-event", newEvent)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "slack-event",
		Label:       "Slack Event",
		Category:    core.CategoryProcessor,
		Description: "Normalize a verified Slack event and filter it by type and an optional predicate.",
		Config:      reflect.TypeFor[eventSettings](),
	})
}

// eventCallbackType is the "type" of Slack's event delivery envelope.
const eventCallbackType = "event_callback"

// eventSettings is the slack-event block's typed configuration.
type eventSettings struct {
	// Allowlist of event types (e.g. app_mention, message); empty allows all.
	EventTypes []string `json:"eventTypes" octo:"label=Event types"`
	// CEL predicate over the normalized body; drop the event when false.
	Filter string `json:"filter" octo:"label=Filter,type=cel"`
}

// eventProcessor normalizes and filters Slack events.
type eventProcessor struct {
	eventTypes map[string]struct{}
	filter     *expr.Program
	env        map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newEvent(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg eventSettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}

	var allow map[string]struct{}
	if len(cfg.EventTypes) > 0 {
		allow = make(map[string]struct{}, len(cfg.EventTypes))
		for _, t := range cfg.EventTypes {
			allow[t] = struct{}{}
		}
	}
	filter, err := compileOptional(deps.Resources, cfg.Filter)
	if err != nil {
		return nil, fmt.Errorf("slack-event: compile filter: %w", err)
	}

	return &eventProcessor{
		eventTypes: allow,
		filter:     filter,
		env:        expr.EnvActivation(deps.Env),
	}, nil
}

// Process unwraps and normalizes a Slack event_callback, dropping the message
// when it is not an event, its type is not allowed, or the filter is false.
func (p *eventProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	body, ok := msg.Body.(map[string]any)
	if !ok {
		return nil, nil
	}
	if t, _ := body["type"].(string); t != eventCallbackType {
		return nil, nil
	}
	inner, ok := body["event"].(map[string]any)
	if !ok {
		return nil, nil
	}

	normalized := normalizeEvent(body, inner)
	if p.eventTypes != nil {
		t, _ := normalized["type"].(string)
		if _, allowed := p.eventTypes[t]; !allowed {
			return nil, nil
		}
	}
	msg.SetBody(normalized)

	if p.filter != nil {
		keep, err := p.evalFilter(msg)
		if err != nil {
			return nil, err
		}
		if !keep {
			return nil, nil
		}
	}
	return msg, nil
}

// evalFilter evaluates the filter predicate against the normalized message,
// requiring a boolean result.
func (p *eventProcessor) evalFilter(msg *types.Message) (bool, error) {
	value, err := p.filter.Eval(expr.MessageActivation(msg, p.env))
	if err != nil {
		return false, fmt.Errorf("slack-event filter: %w", err)
	}
	keep, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("slack-event filter must evaluate to a bool, got %T", value)
	}
	return keep, nil
}

// normalizeEvent flattens a Slack event_callback into a friendly shape, keeping
// the untouched inner event under "raw" for anything the flat fields omit.
func normalizeEvent(envelope, event map[string]any) map[string]any {
	return map[string]any{
		"type":     event["type"],
		"user":     event["user"],
		"channel":  event["channel"],
		"text":     event["text"],
		"ts":       event["ts"],
		"threadTs": event["thread_ts"],
		"botId":    event["bot_id"],
		"teamId":   envelope["team_id"],
		"eventId":  envelope["event_id"],
		"raw":      event,
	}
}
