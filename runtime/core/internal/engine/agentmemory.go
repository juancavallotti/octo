// Agent memory: per-thread conversation transcripts for the ai-agent block,
// persisted in the runtime KV store. An ai-agent with a memoryThreadId loads the
// prior transcript before its run and saves the accumulated transcript after,
// compacting it when it grows past its token budget. The clear-agent-memory leaf
// block wipes a thread. All memory objects live in the user namespace under a
// dedicated prefix so they never collide with object-read/write keys.
package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/expr"
	"github.com/juancavallotti/octo/runtime/types"
)

// Compaction strategies for ai-agent memory.
const (
	memoryCompactPrune     = "prune"     // drop the oldest turns (default)
	memoryCompactSummarize = "summarize" // fold the oldest turns into a summary
)

// defaultMemoryMaxTokens is the estimated-token budget applied to agent memory
// when a memory-enabled ai-agent does not set one.
const defaultMemoryMaxTokens = 8000

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

// loadMemory reads the stored state for a thread. A missing thread yields the
// zero envelope (a fresh conversation).
func loadMemory(ctx context.Context, threadID string) (memoryEnvelope, error) {
	kv := core.RuntimeServicesFromContext(ctx).KV()
	entry, ok, err := kv.Get(ctx, core.NamespaceUser, memoryKey(threadID))
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
func saveMemory(ctx context.Context, threadID string, env memoryEnvelope) error {
	env.Version = memoryVersion
	encoded, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("encode memory: %w", err)
	}
	kv := core.RuntimeServicesFromContext(ctx).KV()
	key := memoryKey(threadID)
	for attempt := 0; attempt < memoryWriteAttempts; attempt++ {
		entry, _, getErr := kv.Get(ctx, core.NamespaceUser, key)
		if getErr != nil {
			return getErr
		}
		if _, setErr := kv.Set(ctx, core.NamespaceUser, key, encoded, entry.Version); setErr != nil {
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
		chars += len(msgs[i].Text)
		for _, t := range msgs[i].Thinking {
			chars += len(t.Text) + len(t.Redacted)
		}
		for _, c := range msgs[i].ToolCalls {
			chars += len(c.Input)
		}
		for _, r := range msgs[i].ToolResults {
			chars += len(r.Content)
		}
	}
	return chars / charsPerToken
}

// compactMemory shrinks transcript to fit maxTokens using the given strategy,
// returning the (possibly shortened) transcript. A non-positive budget or an
// already-fitting transcript is returned unchanged.
//
// msg is the message the agent is running for; only the summarize strategy calls
// the model, and only so its record joins the trace that paid for it.
func compactMemory(
	ctx context.Context, caller *llmCaller, msg *types.Message,
	transcript []core.LLMMessage, maxTokens int, strategy string,
) []core.LLMMessage {
	if maxTokens <= 0 || estimateTokens(transcript) <= maxTokens {
		return transcript
	}
	if strategy == memoryCompactSummarize {
		return summarizeMemory(ctx, caller, msg, transcript, maxTokens)
	}
	return pruneMemory(transcript, maxTokens)
}

// pruneMemory drops the oldest messages until the transcript fits the budget,
// keeping at least one message and never leaving a leading tool turn (whose
// originating assistant tool call was just dropped).
func pruneMemory(msgs []core.LLMMessage, maxTokens int) []core.LLMMessage {
	for len(msgs) > 1 && estimateTokens(msgs) > maxTokens {
		msgs = msgs[1:]
		for len(msgs) > 1 && msgs[0].Role == core.LLMRoleTool {
			msgs = msgs[1:]
		}
	}
	return msgs
}

// summarizeMemory keeps the most recent turns that fit half the budget and folds
// the older turns into a single summary message the model reads as context. It
// falls back to pruning if the model cannot produce a summary.
func summarizeMemory(
	ctx context.Context, caller *llmCaller, msg *types.Message,
	transcript []core.LLMMessage, maxTokens int,
) []core.LLMMessage {
	keepBudget := maxTokens / 2
	cut := 0
	for cut < len(transcript) && estimateTokens(transcript[cut:]) > keepBudget {
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
		return pruneMemory(transcript, maxTokens)
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
	if err := kv.Delete(ctx, core.NamespaceUser, memoryKey(threadID), 0); err != nil {
		return nil, fmt.Errorf("clear-agent-memory %q: %w", threadID, err)
	}
	return msg, nil
}
