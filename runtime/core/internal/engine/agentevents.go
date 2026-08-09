// The ai-agent's observer path: what an agent reports about its own run, and the
// sub-flow that receives it.
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The agent event vocabulary. Half of it mirrors what the provider streams; the
// other half is the agent's own, and is the half a provider can never report —
// it never learns what an octo tool branch did with the arguments it asked for.
const (
	// eventTurnStart is emitted before each model call.
	eventTurnStart = "turn_start"
	// eventText, eventThinking, eventToolInput and eventCustom are the provider's
	// stream events, and arrive only when the block streams.
	eventText      = "text"
	eventThinking  = "thinking"
	eventToolInput = "tool_input"
	eventCustom    = "custom"
	// eventToolCall is the model asking for a tool, with its arguments complete.
	eventToolCall = "tool_call"
	// eventToolResult is what the branch that ran it returned.
	eventToolResult = "tool_result"
	// eventTurnEnd is a finished model turn, with its stop reason and usage.
	eventTurnEnd = "turn_end"
	// eventGuardrail is the agent giving up and taking the guardrail.
	eventGuardrail = "guardrail"
	// eventDone is the agent finishing with an answer.
	eventDone = "done"
	// eventError is a model call that failed.
	eventError = "error"
)

// Field names an event body uses under more than one event type. They are named
// so the body shape stays consistent across the builders below — a consumer
// filtering on body.tool should not have to know which event it came from.
const (
	fieldText       = "text"
	fieldTool       = "tool"
	fieldToolCallID = "toolCallId"
)

// agentEventKinds is the closed set an emit list may name.
var agentEventKinds = map[string]bool{
	eventTurnStart: true, eventText: true, eventThinking: true, eventToolInput: true,
	eventCustom: true, eventToolCall: true, eventToolResult: true, eventTurnEnd: true,
	eventGuardrail: true, eventDone: true, eventError: true,
}

// deltaKinds maps the provider's canonical stream vocabulary onto agent event
// types. They are deliberately spelled the same: a consumer should not have to
// learn two names for one thing.
var deltaKinds = map[core.LLMStreamKind]string{
	core.LLMStreamText:      eventText,
	core.LLMStreamThinking:  eventThinking,
	core.LLMStreamToolInput: eventToolInput,
	core.LLMStreamCustom:    eventCustom,
}

// errEventStop signals that the events path asked the run to stop. It travels as
// an error only because that is the one channel a provider's stream callback has;
// the agent unwraps it into an ordinary stop rather than a failure.
var errEventStop = errors.New("ai-agent events path requested stop")

// emitter runs an ai-agent's events sub-flow.
//
// A nil emitter is inert, so the agent reports unconditionally and a block with no
// events path pays nothing for the calls.
type emitter struct {
	flow *Flow
	// kinds is the emit allow-list, or nil to emit every kind.
	kinds map[string]bool
}

// wants reports whether a kind reaches the sub-flow at all.
//
// It is checked before an event is built rather than after, so an excluded kind
// costs one map lookup — no fields, no message, no invocation. That is what makes
// emit a real lever on a token stream instead of just a filter with extra steps.
func (e *emitter) wants(kind string) bool {
	if e == nil {
		return false
	}
	return e.kinds == nil || e.kinds[kind]
}

// send runs the sub-flow for one event and reports whether the run should stop.
func (e *emitter) send(ctx context.Context, parent *types.Message, kind string, fields map[string]any) bool {
	if !e.wants(kind) {
		return false
	}
	msg, err := e.message(parent, kind, fields)
	if err != nil {
		slog.Debug("ai-agent could not build an event", "kind", kind, "error", err)
		return false
	}
	out, err := e.flow.Process(ctx, msg)
	if err != nil {
		// The path reports on the run; it does not get to fail it. A broken sink is
		// worth a log line, not worth discarding the work the agent has already done.
		slog.Debug("ai-agent events path failed", "kind", kind, "error", err)
		return false
	}
	// A stop is the one thing that travels back. It means nobody is listening any
	// more — an sse-event whose caller hung up is the ordinary cause — and carrying
	// on would spend a whole agent run reporting to no one.
	return out != nil && out.StopRequested()
}

// message builds the event's own message: a fresh one carrying the agent's
// variables, with the event as its body.
//
// It is deliberately not the agent's message. Tool branches run on that one and
// overwrite its body with each call's arguments, so handing it over would both
// corrupt the loop and hand the sub-flow a body that is not the event. It is
// deliberately not a Clone either: Clone deep-copies the body, a cost that would
// be paid once per fragment on a token stream.
//
// Carrying the variables over is what makes the path useful with no configuration
// at all — vars.sseStream travels with them, so an sse-event in the path writes to
// the caller that started the run.
func (e *emitter) message(parent *types.Message, kind string, fields map[string]any) (*types.Message, error) {
	msg, err := types.NewMessage(parent.CorrelationID)
	if err != nil {
		return nil, err
	}
	for name, v := range parent.Variables {
		msg.Variables.Set(name, v)
	}
	body := make(map[string]any, len(fields)+1)
	body["type"] = kind
	for name, v := range fields {
		body[name] = v
	}
	msg.Body = body
	return msg, nil
}

// emitKinds validates an emit list into a lookup, returning nil for an empty list
// so every kind passes.
func emitKinds(emit []string) (map[string]bool, error) {
	if len(emit) == 0 {
		return nil, nil
	}
	kinds := make(map[string]bool, len(emit))
	for _, kind := range emit {
		if !agentEventKinds[kind] {
			return nil, fmt.Errorf("ai-agent emit %q is not an event type (one of: %s)", kind, knownEventKinds())
		}
		kinds[kind] = true
	}
	return kinds, nil
}

// knownEventKinds lists the vocabulary for an error message, sorted so the same
// bad config always reports the same way.
func knownEventKinds() string {
	names := make([]string, 0, len(agentEventKinds))
	for kind := range agentEventKinds {
		names = append(names, kind)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// deltaFields describes one provider stream event.
func deltaFields(ev core.LLMStreamEvent) map[string]any {
	fields := map[string]any{fieldText: ev.Text, "index": ev.Index}
	if ev.Name != "" {
		fields["name"] = ev.Name
	}
	if ev.Tool != "" {
		fields[fieldTool] = ev.Tool
	}
	if ev.ToolCallID != "" {
		fields[fieldToolCallID] = ev.ToolCallID
	}
	return fields
}

// turnEndFields describes a finished model turn.
func turnEndFields(resp *core.LLMResponse) map[string]any {
	fields := map[string]any{fieldText: resp.Text, "stopReason": string(resp.StopReason)}
	if resp.Usage != nil {
		fields["usage"] = map[string]any{
			"inputTokens":    resp.Usage.InputTokens,
			"outputTokens":   resp.Usage.OutputTokens,
			"thinkingTokens": resp.Usage.ThinkingTokens,
			"cachedTokens":   resp.Usage.CachedTokens,
		}
	}
	return fields
}

// callFields describes a tool the model asked for.
func callFields(call core.LLMToolCall) map[string]any {
	return map[string]any{
		fieldTool:       call.Name,
		fieldToolCallID: call.ID,
		"input":         decodeJSON(call.Input),
	}
}

// resultFields describes what the branch that ran a tool returned.
func resultFields(call core.LLMToolCall, res core.LLMToolResult) map[string]any {
	return map[string]any{
		fieldTool:       call.Name,
		fieldToolCallID: call.ID,
		"output":        decodeJSON([]byte(res.Content)),
		"isError":       res.IsError,
	}
}

// decodeJSON renders a payload as structured data where it can, so a CEL
// expression in the events path reads body.input.orderId rather than re-parsing a
// string. Anything that is not JSON is passed through as the string it is.
func decodeJSON(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}
