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
	// eventSignal is something posted to the run from outside it: context folded
	// into the conversation, or a stop. It is the only event describing an
	// instruction the agent did not derive from its own work.
	eventSignal = "signal"
	// eventCompactionStart and eventCompactionEnd bracket the agent shrinking its
	// own conversation to stay inside its token budget. They are the one pair
	// describing something the agent did to itself rather than to the model or a
	// tool, and without them a conversation losing its early turns is invisible.
	//
	// Bracketed rather than reported once, for the same reason turns are: the
	// summarize strategy makes a model call, so compaction can take seconds, and a
	// progress UI with only an after-the-fact event shows a stall it cannot
	// explain.
	eventCompactionStart = "compaction_start"
	eventCompactionEnd   = "compaction_end"
	// eventThreadTitle is the conversation being named, which happens once, on the
	// run that opened it. It is reported rather than left to be discovered because
	// the name is written after the answer has already streamed: a panel that
	// only learns titles from a listing shows the conversation it is in the middle
	// of as nameless until someone reloads it.
	eventThreadTitle = "thread_title"
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
	eventCompactionStart: true, eventCompactionEnd: true, eventSignal: true,
	eventThreadTitle: true, eventGuardrail: true, eventDone: true, eventError: true,
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

// turnEndFields describes a finished model turn, including how full the context
// now is against what it is allowed to be.
//
// The gauge is exact rather than estimated, and it is the whole of what the turn
// leaves behind: the prompt the provider read, plus the reply it produced, which
// is now part of the conversation the next turn will send. maxTokens is zero for
// a block with no budget, and the gauge is then left off entirely — half a gauge
// is worse than none.
func turnEndFields(resp *core.LLMResponse, maxTokens int) map[string]any {
	fields := map[string]any{fieldText: resp.Text, "stopReason": string(resp.StopReason)}
	if resp.Usage == nil {
		return fields
	}
	fields["usage"] = usageFields(resp.Usage)
	if maxTokens > 0 {
		for name, value := range contextFields(resp.Usage.PromptTokens+resp.Usage.OutputTokens, maxTokens) {
			fields[name] = value
		}
	}
	return fields
}

// contextFields is how full the context is and how full it may get.
//
// Both numbers, always together: "used" on its own is unreadable, because the
// budget is per block and a reader has no other way to know whether 12,000 is
// comfortable or one turn from being compacted. It rides on every turn_end as
// well as the compaction pair, so a progress UI can show the gauge filling
// rather than only the moment it overflowed.
func contextFields(used, budget int) map[string]any {
	return map[string]any{"contextTokens": used, "contextMaxTokens": budget}
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
