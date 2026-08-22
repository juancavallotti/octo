// Keeping an agent's prompt inside its budget while the run is still going.
//
// Compacting on the way to storage cannot prevent an overflow: by the time the
// transcript is saved, the turn that was too big has already been sent and
// refused. With the provider's own token counts in hand the agent can see the
// next turn coming, so it compacts before making it.
package engine

import (
	"context"
	"log/slog"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// fitContext shrinks the conversation if the next turn would exceed the budget,
// and returns the messages to send.
//
// It does nothing until a turn has been measured: before that there is no
// overhead figure, and compacting against the raw estimate would cut a
// conversation that has plenty of room. The first turn of a run is never the
// problem anyway — it is the tenth tool result that is.
func (a *aiAgent) fitContext(
	ctx context.Context, msg *types.Message, messages []core.LLMMessage, iter int, meter *contextMeter,
) []core.LLMMessage {
	if meter.measured() == 0 || a.contextMaxTokens <= 0 {
		return messages
	}
	before := meter.predict(estimateTokens(messages))
	if before <= a.contextMaxTokens {
		return messages
	}

	// Two ends of the conversation are not the compactor's to touch. The trailing
	// tool turn and the assistant turn that asked for it are one unit — providers
	// require the results to follow the call — and the opening turn is what makes
	// the whole thing a valid conversation, since it has to start on a user turn.
	// So only the middle is compacted, which is where the weight is anyway.
	keep := protectedSuffix(messages)
	head, opening := messages[:keep], 0
	if len(messages) > 0 && messages[0].Role == core.LLMRoleUser {
		head, opening = messages[1:keep], 1
	}
	if len(head) == 0 {
		return messages // nothing between the two ends to give back
	}

	fixed := meter.sizeOf(estimateTokens(messages[:opening])) +
		meter.sizeOf(estimateTokens(messages[keep:]))
	budget := a.contextMaxTokens - fixed
	if budget <= 0 {
		slog.Warn("ai-agent cannot fit its context: the current turn alone exceeds the budget",
			"block", a.name, "tokens", before, "budget", a.contextMaxTokens)
		return messages
	}

	compacted := compactMemory(ctx, a.caller, msg, head, budget, a.memoryCompaction, meter)
	// Copied rather than appended in place: compactMemory returns a sub-slice of
	// the head, whose capacity runs on into the protected tail, so appending to it
	// would write over the very messages this is preserving.
	out := make([]core.LLMMessage, 0, opening+len(compacted)+len(messages)-keep)
	out = append(out, messages[:opening]...)
	out = append(out, compacted...)
	out = append(out, messages[keep:]...)

	after := meter.predict(estimateTokens(out))
	slog.Info("ai-agent compacted its context", "block", a.name,
		"strategy", a.memoryCompaction, "before", before, "after", after,
		"dropped", len(messages)-len(out))
	a.report(ctx, msg, iter, eventCompaction, map[string]any{
		"strategy": a.memoryCompaction,
		"before":   before,
		"after":    after,
		"dropped":  len(messages) - len(out),
	})
	return out
}

// protectedSuffix returns the index from which messages must be kept intact: the
// trailing tool turns and the assistant turn whose calls they answer.
//
// It returns len(msgs) when the conversation ends on an ordinary turn, which
// protects nothing and lets compaction work on all of it.
func protectedSuffix(msgs []core.LLMMessage) int {
	i := len(msgs)
	for i > 0 && msgs[i-1].Role == core.LLMRoleTool {
		i--
	}
	if i == len(msgs) {
		return i // no trailing tool turn, so nothing is paired
	}
	if i > 0 && len(msgs[i-1].ToolCalls) > 0 {
		i--
	}
	return i
}
