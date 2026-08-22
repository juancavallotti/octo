package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// memoryAgentConfig builds a memory-enabled ai-agent bound to the given thread id
// (a CEL string literal), with a single no-op tool so the build validates.
func memoryAgentConfig(threadIDExpr string) types.BlockConfig {
	return types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "chat",
		Tools:          []types.ToolConfig{toolBranch("noop", "does nothing", nil)},
		MemoryThreadID: threadIDExpr,
	}
}

// assistantText returns whether the messages carry an assistant turn with text.
func hasAssistantText(msgs []core.LLMMessage, text string) bool {
	for i := range msgs {
		if msgs[i].Role == core.LLMRoleAssistant && msgs[i].Text == text {
			return true
		}
	}
	return false
}

func TestAIAgentMemoryRoundTrips(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	cfg := memoryAgentConfig(`"t1"`)

	// Run 1: the agent finishes immediately; its transcript is saved to thread t1.
	fake1 := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("first-answer")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake1), cfg).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	stored, err := loadMemory(ctx, "t1")
	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if !hasAssistantText(stored.Messages, "first-answer") {
		t.Fatalf("thread t1 did not persist the first answer: %+v", stored)
	}

	// Run 2 on the same thread must replay the prior transcript to the model.
	fake2 := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("second-answer")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake2), cfg).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	first := fake2.calls[0].Messages
	if len(first) < 3 {
		t.Fatalf("run 2 first request carried %d messages, want prior transcript + new input", len(first))
	}
	if !hasAssistantText(first, "first-answer") {
		t.Errorf("run 2 first request did not replay the prior answer: %+v", first)
	}
}

func TestAIAgentMemoryThreadsAreIsolated(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any

	fake1 := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("t1-answer")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake1), memoryAgentConfig(`"t1"`)).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("thread t1 run: %v", err)
	}

	// A different thread starts fresh: its first request is only the new input.
	fake2 := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("t2-answer")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake2), memoryAgentConfig(`"t2"`)).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("thread t2 run: %v", err)
	}
	if got := len(fake2.calls[0].Messages); got != 1 {
		t.Errorf("thread t2 first request carried %d messages, want 1 (no cross-thread history)", got)
	}
}

func TestAIAgentMemoryDisabledWhenNoThread(t *testing.T) {
	// Without a memoryThreadId the agent never touches the KV: it runs with a plain
	// context that carries no runtime services.
	var seen []any
	cfg := types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "chat",
		Tools: []types.ToolConfig{toolBranch("noop", "does nothing", nil)},
	}
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("ok")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).
		Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("stateless agent should not require services: %v", err)
	}
}

func TestAIAgentMemoryRejectsBadCompaction(t *testing.T) {
	cfg := memoryAgentConfig(`"t1"`)
	cfg.MemoryCompaction = "nope"
	if _, err := (&builder{reg: testRegistry(), deps: depsLLM(&scriptedLLM{})}).block(cfg); err == nil {
		t.Error("expected an error for an unknown memoryCompaction value")
	}
}

func TestClearAgentMemory(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	seed := memoryEnvelope{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}}
	if err := saveMemory(ctx, "t1", seed); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	clearBlock, err := newClearAgentMemory(types.Settings{"threadId": `"t1"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newClearAgentMemory: %v", err)
	}
	if _, err := clearBlock.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("clear Process: %v", err)
	}
	if got, _ := loadMemory(ctx, "t1"); got.Messages != nil {
		t.Errorf("memory not cleared: %+v", got)
	}
	// Idempotent: clearing a missing thread is not an error.
	if _, err := clearBlock.Process(ctx, mustMessage(t)); err != nil {
		t.Errorf("second clear should be a no-op, got %v", err)
	}
}

func TestEstimateTokens(t *testing.T) {
	got := estimateTokens([]core.LLMMessage{
		{Text: "12345678"},
		{ToolResults: []core.LLMToolResult{{Content: "abcd"}}},
	})
	if got != 3 { // (8 + 4) / 4
		t.Errorf("estimateTokens = %d, want 3", got)
	}
}

func TestPruneMemoryFitsBudget(t *testing.T) {
	msgs := []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: strings.Repeat("a", 400)},
		{Role: core.LLMRoleAssistant, Text: strings.Repeat("b", 400)},
		{Role: core.LLMRoleUser, Text: "tiny"},
	}
	out := pruneMemory(msgs, 50, newContextMeter())
	if len(out) == 0 {
		t.Fatal("prune dropped everything")
	}
	if estimateTokens(out) > 50 {
		t.Errorf("pruned transcript still over budget: %d tokens", estimateTokens(out))
	}
	if out[0].Role == core.LLMRoleTool {
		t.Error("prune left a leading orphaned tool turn")
	}
	if len(out) >= len(msgs) {
		t.Error("prune did not drop any messages")
	}
}

func TestSummarizeMemoryFoldsOldTurns(t *testing.T) {
	msgs := []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: strings.Repeat("a", 400)},
		{Role: core.LLMRoleAssistant, Text: strings.Repeat("b", 400)},
		{Role: core.LLMRoleUser, Text: "tiny"},
	}
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("SUMMARY")}}
	out := summarizeMemory(context.Background(), bareCaller(fake), mustMessage(t), msgs, 50, newContextMeter())
	if len(out) >= len(msgs) {
		t.Errorf("summarize did not shrink the transcript: %d messages", len(out))
	}
	if !strings.Contains(out[0].Text, "SUMMARY") {
		t.Errorf("first message is not the summary: %q", out[0].Text)
	}
}

// TestCompactMemoryNoopUnderBudget also pins that the prune path reaches neither
// the model nor the message: both are nil here, and compacting must not touch
// either to decide there is nothing to do.
func TestCompactMemoryNoopUnderBudget(t *testing.T) {
	msgs := []core.LLMMessage{{Role: core.LLMRoleUser, Text: "small"}}
	meter := newContextMeter()
	if out := compactMemory(context.Background(), nil, nil, msgs, 100000, memoryCompactPrune, meter); len(out) != len(msgs) {
		t.Errorf("compact changed a transcript already under budget: %+v", out)
	}
	if out := compactMemory(context.Background(), nil, nil, msgs, 0, memoryCompactPrune, meter); len(out) != len(msgs) {
		t.Errorf("compact with a zero budget should be a no-op: %+v", out)
	}
}

// The stored form carries the measured size beside the transcript, so a resumed
// conversation starts from the provider's own count instead of re-guessing it.
func TestMemoryEnvelopeRoundTrips(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	want := memoryEnvelope{
		Tokens:   4242,
		Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}},
	}
	if err := saveMemory(ctx, "t1", want); err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	got, err := loadMemory(ctx, "t1")
	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if got.Tokens != want.Tokens || len(got.Messages) != 1 || got.Messages[0].Text != "hi" {
		t.Errorf("envelope = %+v, want %+v", got, want)
	}
	if got.Version != memoryVersion {
		t.Errorf("version = %d, want %d — saveMemory stamps it, callers do not", got.Version, memoryVersion)
	}
}

// Every thread written before the envelope existed holds a bare transcript
// array. It has to keep loading, with no measured size, rather than failing to
// decode and taking a live conversation with it.
func TestLoadMemoryAcceptsALegacyBareArray(t *testing.T) {
	ctx, kv := withFakeServices(context.Background())
	legacy, err := json.Marshal([]core.LLMMessage{
		{Role: core.LLMRoleUser, Text: "older"},
		{Role: core.LLMRoleAssistant, Text: "answer"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := kv.Set(ctx, core.NamespaceUser, memoryKey("t1"), legacy, 0); err != nil {
		t.Fatalf("seed legacy memory: %v", err)
	}

	got, err := loadMemory(ctx, "t1")
	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if len(got.Messages) != 2 || !hasAssistantText(got.Messages, "answer") {
		t.Errorf("legacy transcript = %+v, want both messages", got.Messages)
	}
	if got.Tokens != 0 {
		t.Errorf("tokens = %d, want 0 — a legacy transcript was never measured", got.Tokens)
	}
}

func TestDecodeMemoryRejectsGarbage(t *testing.T) {
	for _, raw := range []string{"not json", "[{", "{\"messages\":"} {
		if _, err := decodeMemory([]byte(raw)); err == nil {
			t.Errorf("decodeMemory(%q) succeeded, want a decode error", raw)
		}
	}
}

// The size a run measured is carried into storage, so the next run on the thread
// starts from the provider's own count instead of re-deriving one from
// characters. It stores the conversation's own contribution, not the whole
// prompt: the next run supplies its own system prompt and tools.
func TestAgentStoresTheMeasuredSize(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	cfg := memoryAgentConfig(`"t1"`)
	cfg.ContextMaxTokens = 100000

	var seen []any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(endTurnResp(strings.Repeat("a", 400)),
			&core.LLMUsage{PromptTokens: 50000, OutputTokens: 4}),
	}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	stored, err := loadMemory(ctx, "t1")
	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if stored.Tokens == 0 || stored.Tokens >= 50000 {
		t.Errorf("stored tokens = %d, want the conversation's own size, not the whole prompt", stored.Tokens)
	}
	if len(stored.Messages) == 0 {
		t.Error("transcript was not stored")
	}
}

// A budget with no room for even one exchange must not throw the conversation
// away. The most recent exchange is kept over budget, and compactMemory logs
// that it does not fit — the fix is the configuration, not the transcript.
func TestPruneMemoryKeepsTheLastExchangeRatherThanNothing(t *testing.T) {
	msgs := []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: strings.Repeat("a", 400)},
		{Role: core.LLMRoleAssistant, Text: strings.Repeat("b", 400)},
	}
	meter := newContextMeter()
	meter.observe(estimateTokens(msgs), 90000) // an overhead nothing can fit under

	out := pruneMemory(msgs, 10, meter)
	if len(out) == 0 {
		t.Fatal("prune discarded the whole conversation")
	}
	if out[0].Role != core.LLMRoleUser {
		t.Errorf("transcript starts on %s, want a user turn", out[0].Role)
	}
}

// A provider that reports no usage leaves the agent exactly where it started: on
// the chars/4 estimate, with an unfitted meter that passes it straight through.
// Degrading to zero would read as "the context is empty" and compact nothing,
// ever.
func TestUnfittedMeterCompactsOnTheEstimateAlone(t *testing.T) {
	msgs := []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: strings.Repeat("a", 400)},
		{Role: core.LLMRoleAssistant, Text: strings.Repeat("b", 400)},
		{Role: core.LLMRoleUser, Text: "tiny"},
	}
	meter := newContextMeter() // nothing observed: predict is the estimate itself
	if got := meter.predict(estimateTokens(msgs)); got != estimateTokens(msgs) {
		t.Fatalf("unfitted predict = %d, want the raw estimate %d", got, estimateTokens(msgs))
	}

	out := pruneMemory(msgs, 50, meter)
	if len(out) >= len(msgs) {
		t.Errorf("the estimate alone did not compact: %d messages", len(out))
	}
	if estimateTokens(out) > 50 {
		t.Errorf("pruned transcript still over budget: %d tokens", estimateTokens(out))
	}
	if out[0].Role != core.LLMRoleUser {
		t.Errorf("transcript starts on %s, want a user turn", out[0].Role)
	}
}

// Flow YAML is decoded permissively, so a renamed key that nothing rejects is a
// budget silently reverting to the default. The build has to say so.
func TestAgentRejectsTheOldMemoryMaxTokensKey(t *testing.T) {
	cfg := memoryAgentConfig(`"t1"`)
	cfg.MemoryMaxTokens = 4000

	_, err := (&builder{reg: testRegistry(), deps: depsLLM(&scriptedLLM{})}).block(cfg)
	if err == nil {
		t.Fatal("expected an error for the old memoryMaxTokens key")
	}
	if !strings.Contains(err.Error(), "contextMaxTokens") {
		t.Errorf("error = %q, want it to name the replacement", err)
	}
}
