// This file provides the "notion-event" block: it normalizes a verified Notion
// webhook into a friendly, flat shape and filters it. Events whose type is not in
// the eventTypes allowlist, or that fail the optional filter CEL predicate, are
// dropped so the flow only handles what it cares about. Signature verification is
// a separate concern handled upstream by notion-verify-request.
package notion

import (
	"context"
	"fmt"
	"reflect"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

func registerEvent() {
	core.MustRegisterBlock("notion-event", newEvent)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:     "notion-event",
		Label:    "Notion Event",
		Category: core.CategoryProcessor,
		Description: "Normalize a verified Notion webhook and filter it by type and an optional " +
			"predicate.",
		Config: reflect.TypeFor[eventSettings](),
	})
}

// eventSettings is the notion-event block's typed configuration.
type eventSettings struct {
	// Allowlist of event types (e.g. page.created, page.content_updated); empty
	// allows all.
	EventTypes []string `json:"eventTypes" octo:"label=Event types"`
	// CEL predicate over the normalized body; drop the event when false.
	Filter string `json:"filter" octo:"label=Filter,type=cel"`
}

// eventProcessor normalizes and filters Notion webhook events.
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
		return nil, fmt.Errorf("notion-event: compile filter: %w", err)
	}

	return &eventProcessor{
		eventTypes: allow,
		filter:     filter,
		env:        expr.EnvActivation(deps.Env),
	}, nil
}

// Process normalizes a Notion webhook, dropping the message when its body is not
// an object, its type is not allowed, or the filter is false.
func (p *eventProcessor) Process(_ context.Context, msg *types.Message) (*types.Message, error) {
	body, ok := msg.Body.(map[string]any)
	if !ok {
		return nil, nil
	}

	normalized := normalizeEvent(body)
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
		return false, fmt.Errorf("notion-event filter: %w", err)
	}
	keep, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("notion-event filter must evaluate to a bool, got %T", value)
	}
	return keep, nil
}

// normalizeEvent flattens a Notion webhook into a friendly shape, keeping the
// untouched payload under "raw" for anything the flat fields omit. Notion delivers
// event fields at the top level (there is no event_callback envelope), with the
// affected object under "entity".
func normalizeEvent(event map[string]any) map[string]any {
	var entityID, entityType any
	if entity, ok := event["entity"].(map[string]any); ok {
		entityID = entity["id"]
		entityType = entity["type"]
	}
	return map[string]any{
		"type":        event["type"],
		"entityId":    entityID,
		"entityType":  entityType,
		"workspaceId": event["workspace_id"],
		"timestamp":   event["timestamp"],
		"raw":         event,
	}
}
