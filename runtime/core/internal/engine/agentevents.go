// The ai-agent's observer path: what an agent reports about its own run, and the
// sub-flow that receives it.
package engine

import (
	"encoding/json"
	"errors"

	"github.com/juancavallotti/octo/runtime/core"
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
	// eventCompaction is the agent shrinking its own conversation to stay inside
	// its token budget. It is the one event describing something the agent did to
	// itself rather than to the model or a tool, and without it a conversation
	// losing its early turns is invisible.
	eventCompaction = "compaction"
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
	eventCompaction: true, eventGuardrail: true, eventDone: true, eventError: true,
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
		fields["usage"] = usageFields(resp.Usage)
	}
	return fields
}

// usageFields projects a turn's token accounting. It is shared by the events path
// and the trace record so the two observers of one turn cannot report it
// differently — and so a change to the shape lands in both at once.
func usageFields(usage *core.LLMUsage) map[string]any {
	fields := map[string]any{
		"inputTokens":      usage.InputTokens,
		"outputTokens":     usage.OutputTokens,
		"thinkingTokens":   usage.ThinkingTokens,
		"cachedTokens":     usage.CachedTokens,
		"cacheWriteTokens": usage.CacheWriteTokens,
		"promptTokens":     usage.PromptTokens,
	}
	// Present only when a provider reported one. Writing it unconditionally would
	// put a zero beside every Anthropic, OpenAI and Gemini turn, and a cost of
	// zero is the one claim the whole accounting path exists never to make by
	// accident.
	if usage.ReportedCostUSD != nil {
		fields["reportedCostUSD"] = *usage.ReportedCostUSD
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
