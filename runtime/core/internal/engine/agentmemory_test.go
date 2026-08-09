package engine

import (
	"context"
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
	if !hasAssistantText(stored, "first-answer") {
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
	if err := saveMemory(ctx, "t1", []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	clearBlock, err := newClearAgentMemory(types.Settings{"threadId": `"t1"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newClearAgentMemory: %v", err)
	}
	if _, err := clearBlock.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("clear Process: %v", err)
	}
	if got, _ := loadMemory(ctx, "t1"); got != nil {
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
	out := pruneMemory(msgs, 50)
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
	out := summarizeMemory(context.Background(), bareCaller(fake), mustMessage(t), msgs, 50)
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
	if out := compactMemory(context.Background(), nil, nil, msgs, 100000, memoryCompactPrune); len(out) != len(msgs) {
		t.Errorf("compact changed a transcript already under budget: %+v", out)
	}
	if out := compactMemory(context.Background(), nil, nil, msgs, 0, memoryCompactPrune); len(out) != len(msgs) {
		t.Errorf("compact with a zero budget should be a no-op: %+v", out)
	}
}
