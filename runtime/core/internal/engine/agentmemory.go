// Agent memory: per-thread conversation transcripts for the ai-agent block,
// persisted in the runtime KV store. An ai-agent with a memoryThreadId loads the
// prior transcript before its run and saves the accumulated transcript after,
// compacting it when it grows past its token budget. The clear-agent-memory leaf
// block wipes a thread. All memory objects live in the user namespace under a
// dedicated prefix so they never collide with object-read/write keys.
//
// The persistent namespace, deliberately: a thread is a conversation somebody is
// having, and a restart that silently forgot what was said would not read as a
// dropped cache entry — it would read as the agent losing the plot mid-sentence.
// There is no volatile option here for the same reason there is no option to
// forget on purpose.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// Compaction strategies for ai-agent memory.
const (
	memoryCompactPrune     = "prune"     // drop the oldest turns (default)
	memoryCompactSummarize = "summarize" // fold the oldest turns into a summary
)

// defaultContextMaxTokens is the prompt budget applied when an ai-agent does not
// set one.
//
// It is double the transcript-only budget it replaces, which keeps roughly the
// same room for conversation now that the figure also covers the system prompt
// and the tool schemas — a few thousand tokens for an agent with a handful of
// tools. Holding the old number against the wider meaning would have quietly
// halved every existing conversation's memory.
const defaultContextMaxTokens = 16000

// memorySaveTimeout bounds saving a transcript. It is generous because the save
// path may summarize, which is a real model call; it exists to stop a wedged
// store holding a flow worker forever, not to keep the save quick.
const memorySaveTimeout = 2 * time.Minute

// memoryKeyPrefix namespaces agent-memory objects in the user KV namespace so
// they never collide with object-read/write keys.
const memoryKeyPrefix = "agent-memory/"

// memoryWriteAttempts bounds the optimistic-concurrency retry loop of a memory
// save, mirroring object-write.
const memoryWriteAttempts = 5

// charsPerToken is the divisor of the chars/token estimate used to size stored
// transcripts. There is no tokenizer in the runtime, so this is an approximation.
const charsPerToken = 4

func registerClearAgentMemory() {
	core.MustRegisterBlock("clear-agent-memory", newClearAgentMemory)

	core.RegisterBlockMeta(core.BlockMeta{
		Type:        "clear-agent-memory",
		Label:       "Clear Agent Memory",
		Category:    core.CategoryProcessor,
		Group:       groupAILLM,
		Icon:        "BrainCog",
		Description: "Erase an ai-agent conversation thread's stored memory by its thread id.",
		Config:      reflect.TypeFor[clearAgentMemorySettings](),
	})
}

// memoryKey returns the KV key for a thread's stored transcript.
func memoryKey(threadID string) string { return memoryKeyPrefix + threadID }

// memoryVersion is the stored envelope's shape. It exists so a future change to
// the stored form can be told apart from this one without guessing.
const memoryVersion = 1

// memoryEnvelope is a thread's stored state: the transcript, and the size the
// provider measured for it.
//
// Tokens is the conversation's own contribution — what contextMeter.sizeOf
// reports, with the run's overhead left out. Storing the raw prompt size would
// bake in a system prompt and a tool set that the next run may not have. Zero
// means "not measured", which is what a transcript written before this envelope
// existed reports, and what a provider that accounts for nothing leaves behind.
type memoryEnvelope struct {
	Version  int               `json:"v"`
	Tokens   int               `json:"tokens"`
	Messages []core.LLMMessage `json:"messages"`
}

// loadMemory reads the stored state for a thread from one KV namespace. A missing
// thread yields the zero envelope (a fresh conversation).
func loadMemory(ctx context.Context, namespace, threadID string) (memoryEnvelope, error) {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	entry, ok, err := kv.Get(ctx, namespace, memoryKey(threadID))
	if err != nil {
		return memoryEnvelope{}, err
	}
	if !ok {
		return memoryEnvelope{}, nil
	}
	return decodeMemory(entry.Value)
}

// decodeMemory reads either stored form.
//
// A payload that opens with '[' is a bare transcript array, which is what every
// thread written before the envelope existed holds. It loads with no measured
// size, so the first turn of the next run re-establishes one.
//
// This is a read-side tolerance for data already on disk, not a second code
// path: there is one writer and one stored form from here on. Delete the sniff
// once no deployment can still be holding a pre-envelope transcript.
func decodeMemory(raw []byte) (memoryEnvelope, error) {
	if trimmed := bytes.TrimLeft(raw, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
		var msgs []core.LLMMessage
		if err := json.Unmarshal(raw, &msgs); err != nil {
			return memoryEnvelope{}, fmt.Errorf("decode memory: %w", err)
		}
		return memoryEnvelope{Version: memoryVersion, Messages: msgs}, nil
	}
	var env memoryEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return memoryEnvelope{}, fmt.Errorf("decode memory: %w", err)
	}
	return env, nil
}

// saveMemory persists a thread's state using optimistic concurrency, re-reading
// the current version and retrying on a conflict (as object-write does).
func saveMemory(ctx context.Context, namespace, threadID string, env memoryEnvelope) error {
	env.Version = memoryVersion
	encoded, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode memory: %w", err)
	}
	kv := core.RuntimeServicesFromContext(ctx).KV()
	key := memoryKey(threadID)
	for attempt := 0; attempt < memoryWriteAttempts; attempt++ {
		entry, _, getErr := kv.Get(ctx, namespace, key)
		if getErr != nil {
			return getErr
		}
		if _, setErr := kv.Set(ctx, namespace, key, encoded, entry.Version); setErr != nil {
			if errors.Is(setErr, core.ErrVersionConflict) {
				continue // a concurrent writer won; re-read and retry
			}
			return setErr
		}
		return nil
	}
	return fmt.Errorf("save memory %q: %w after %d attempts", threadID, core.ErrVersionConflict, memoryWriteAttempts)
}

// estimateTokens approximates the token size of a transcript with a chars/4
// heuristic (there is no tokenizer in the runtime). It counts message text, tool
// call arguments, tool-result content, and reasoning blocks.
//
// Reasoning is counted because it is sent: a tool loop carries thinking blocks
// back untouched, so a transcript that leaves them out of its own accounting
// under-reports itself by however much the model thought. The encrypted halves
// are counted by their bytes, which is the wrong ratio for a blob but a better
// answer than zero — and the contextMeter's fitted scale absorbs the difference,
// since it only ever needs this to be proportional.
func estimateTokens(msgs []core.LLMMessage) int {
	chars := 0
	for i := range msgs {
		m := &msgs[i] // by pointer: a message carries several slices, and this runs per turn
		chars += len(m.Text)
		for _, t := range m.Thinking {
			chars += len(t.Text) + len(t.Redacted)
		}
		for _, c := range m.ToolCalls {
			chars += len(c.Input)
		}
		for _, r := range m.ToolResults {
			chars += len(r.Content)
		}
	}
	return chars / charsPerToken
}

// compactMemory shrinks transcript until the prompt it contributes to fits
// maxTokens, using the given strategy, and returns the (possibly shortened)
// transcript. A non-positive budget or an already-fitting transcript is returned
// unchanged.
//
// The budget is measured against the whole prompt, not the transcript alone: the
// meter carries the run's system prompt and tool schemas as a constant, and those
// occupy the model's window exactly as the conversation does.
//
// msg is the message the agent is running for; only the summarize strategy calls
// the model, and only so its record joins the trace that paid for it.
func compactMemory(
	ctx context.Context, caller *llmCaller, msg *types.Message,
	transcript []core.LLMMessage, maxTokens int, strategy string, meter *contextMeter,
) []core.LLMMessage {
	if maxTokens <= 0 || meter.predict(estimateTokens(transcript)) <= maxTokens {
		return transcript
	}
	var compacted []core.LLMMessage
	if strategy == memoryCompactSummarize {
		compacted = summarizeMemory(ctx, caller, msg, transcript, maxTokens, meter)
	} else {
		compacted = pruneMemory(transcript, maxTokens, meter)
	}
	// Compaction cannot drop the last message, so a budget smaller than the run's
	// fixed overhead has no solution. Say so rather than looping: the fix is the
	// configuration, and the alternative is a provider rejecting every turn with
	// nothing in the logs pointing at why.
	if got := meter.predict(estimateTokens(compacted)); got > maxTokens {
		slog.Warn("ai-agent could not compact its context into the budget",
			"strategy", strategy, "tokens", got, "budget", maxTokens, "overhead", meter.overhead)
	}
	return compacted
}

// pruneMemory drops the oldest messages until the prompt fits the budget, and
// returns a transcript that is still replayable.
func pruneMemory(msgs []core.LLMMessage, maxTokens int, meter *contextMeter) []core.LLMMessage {
	kept := msgs
	for len(kept) > 1 && meter.predict(estimateTokens(kept)) > maxTokens {
		kept = kept[1:]
	}
	if valid := trimToValidStart(kept); len(valid) > 0 {
		return valid
	}
	// Pruning to the budget left nothing a provider would accept, which happens
	// when the budget has no room for even one exchange. Keep the most recent one
	// anyway: it is over budget and replayable, where the alternative is throwing
	// the conversation away over a misconfigured number. compactMemory logs that
	// the result does not fit.
	return lastExchange(msgs)
}

// lastExchange returns the transcript from its final user turn onward, which is
// the shortest suffix a provider will still accept. It is nil for a transcript
// with no user turn at all, which is not a conversation.
func lastExchange(msgs []core.LLMMessage) []core.LLMMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == core.LLMRoleUser {
			return msgs[i:]
		}
	}
	return nil
}

// trimToValidStart drops leading messages until the transcript opens on a user
// turn, which is the shape every provider requires of one.
//
// Two ways pruning breaks that, and they cost differently. A leading tool turn
// is answering a call that has just been dropped, which the providers reject
// outright. A leading assistant turn is subtler — Anthropic requires the first
// message to be a user turn, so a conversation pruned to start mid-exchange is
// refused on its next request, not on the one that pruned it.
//
// Returning nothing is a legitimate answer: a budget with no room for even one
// exchange keeps no conversation, and the caller opens a fresh one.
func trimToValidStart(msgs []core.LLMMessage) []core.LLMMessage {
	for len(msgs) > 0 && msgs[0].Role != core.LLMRoleUser {
		msgs = msgs[1:]
	}
	return msgs
}

// summarizeMemory keeps the most recent turns that fit half the budget and folds
// the older turns into a single summary message the model reads as context. It
// falls back to pruning if the model cannot produce a summary.
func summarizeMemory(
	ctx context.Context, caller *llmCaller, msg *types.Message,
	transcript []core.LLMMessage, maxTokens int, meter *contextMeter,
) []core.LLMMessage {
	// Half of what is left for the conversation, and the conversation's own size
	// against it. predict adds the run's constant overhead — the system prompt and
	// the tool schemas — to every candidate tail, so charging that to the tail and
	// then halving the budget prices the turns out at once: an overhead at or over
	// half the budget makes every tail too big, the loop runs to the end, and the
	// summary replaces the whole transcript including the turn just taken.
	keepBudget := max(0, maxTokens-meter.overhead) / 2
	cut := 0
	for cut < len(transcript) && meter.sizeOf(estimateTokens(transcript[cut:])) > keepBudget {
		cut++
	}
	// Do not let the kept tail start with an orphaned tool turn.
	for cut < len(transcript) && transcript[cut].Role == core.LLMRoleTool {
		cut++
	}
	if cut == 0 {
		return transcript // nothing old enough to summarize
	}

	summary, err := summarizeTurns(ctx, caller, msg, transcript[:cut])
	if err != nil || summary == "" {
		return pruneMemory(transcript, maxTokens, meter)
	}
	tail := transcript[cut:]
	compacted := make([]core.LLMMessage, 0, len(tail)+1)
	compacted = append(compacted, core.LLMMessage{
		Role: core.LLMRoleUser,
		Text: "Summary of earlier conversation:\n" + summary,
	})
	return append(compacted, tail...)
}

// summarizeTurns asks the model to summarize a run of turns into concise prose.
//
// This is a real, billed model call the agent's own turn loop never sees, so it is
// traced like any other — at the agent's own address, because that is where the
// money is spent and where `memoryCompaction: summarize` would be turned off. It
// carries no iteration and is marked with its purpose instead, so it cannot be
// read as one of the turns the agent took.
func summarizeTurns(
	ctx context.Context, caller *llmCaller, msg *types.Message, transcript []core.LLMMessage,
) (string, error) {
	var b strings.Builder
	for i := range transcript {
		m := transcript[i]
		if m.Text != "" {
			fmt.Fprintf(&b, "%s: %s\n", m.Role, m.Text)
		}
		for _, c := range m.ToolCalls {
			fmt.Fprintf(&b, "assistant called tool %s(%s)\n", c.Name, string(c.Input))
		}
		for _, r := range m.ToolResults {
			fmt.Fprintf(&b, "tool result: %s\n", r.Content)
		}
	}
	resp, err := caller.complete(ctx, msg, core.LLMRequest{
		System: "Summarize the following conversation transcript concisely, preserving facts, " +
			"decisions, and any context needed to continue. Respond with the summary only.",
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: b.String()}},
	}, turnLabel{purpose: purposeMemory})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Text), nil
}

// clearAgentMemorySettings configures the clear-agent-memory block.
type clearAgentMemorySettings struct {
	// CEL expression for the thread id whose memory is cleared.
	ThreadID string `json:"threadId" octo:"label=Thread ID,type=cel,required"`
}

// clearAgentMemory removes a thread's stored transcript from the user KV namespace.
type clearAgentMemory struct {
	threadID *expr.Program
	env      map[string]any
}

//nolint:ireturn // a BlockFactory returns the MessageProcessor interface
func newClearAgentMemory(raw types.Settings, deps core.BlockDeps) (core.MessageProcessor, error) {
	var cfg clearAgentMemorySettings
	if err := raw.Decode(&cfg); err != nil {
		return nil, err
	}
	if cfg.ThreadID == "" {
		return nil, errors.New("clear-agent-memory requires a threadId expression")
	}
	threadID, err := expr.CompileMessage(deps.Resources, cfg.ThreadID)
	if err != nil {
		return nil, err
	}
	return &clearAgentMemory{threadID: threadID, env: expr.EnvActivation(deps.Env)}, nil
}

// Process evaluates the thread id and deletes its memory unconditionally (version
// 0), so the clear is idempotent: a missing thread is not an error. The message
// passes through unchanged.
func (p *clearAgentMemory) Process(ctx context.Context, msg *types.Message) (*types.Message, error) {
	threadID, err := p.threadID.EvalString(expr.MessageActivation(msg, p.env))
	if err != nil {
		return nil, fmt.Errorf("clear-agent-memory threadId: %w", err)
	}
	kv := core.RuntimeServicesFromContext(ctx).KV()
	// Both tiers, without asking which one to look in. A thread lives in exactly
	// one of them — whichever its agent declared — and a clear that missed because
	// the block and the agent disagreed about memoryVolatile would report success
	// while leaving the conversation intact, which is the one failure a "forget me"
	// must not have.
	for _, namespace := range []string{core.NamespaceUser, core.NamespaceUserVolatile} {
		if err := kv.Delete(ctx, namespace, memoryKey(threadID), 0); err != nil {
			return nil, fmt.Errorf("clear-agent-memory %q: %w", threadID, err)
		}
	}
	return msg, nil
}
