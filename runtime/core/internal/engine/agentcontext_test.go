package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// A tool turn cannot be separated from the assistant turn whose calls it
// answers: the providers reject a conversation where the two have come apart.
func TestProtectedSuffix(t *testing.T) {
	assistantCalling := core.LLMMessage{
		Role:      core.LLMRoleAssistant,
		ToolCalls: []core.LLMToolCall{{ID: "c1", Name: "lookup"}},
	}
	toolTurn := core.LLMMessage{Role: core.LLMRoleTool}
	user := core.LLMMessage{Role: core.LLMRoleUser}
	plainAssistant := core.LLMMessage{Role: core.LLMRoleAssistant, Text: "done"}

	tests := []struct {
		name string
		msgs []core.LLMMessage
		want int
	}{
		{
			// Ends on an ordinary turn: nothing is paired, so all of it may be cut.
			name: "no trailing tool turn protects nothing",
			msgs: []core.LLMMessage{user, assistantCalling, toolTurn, plainAssistant},
			want: 4,
		},
		{
			name: "a trailing tool turn protects it and its call",
			msgs: []core.LLMMessage{user, plainAssistant, assistantCalling, toolTurn},
			want: 2,
		},
		{
			name: "several trailing tool turns are all protected",
			msgs: []core.LLMMessage{user, assistantCalling, toolTurn, toolTurn},
			want: 1,
		},
		{
			name: "an orphaned trailing tool turn protects only itself",
			msgs: []core.LLMMessage{user, plainAssistant, toolTurn},
			want: 2,
		},
		{name: "empty", msgs: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := protectedSuffix(tt.msgs); got != tt.want {
				t.Errorf("protectedSuffix = %d, want %d", got, tt.want)
			}
		})
	}
}

// compactingAgentConfig builds an agent with one tool the model can call, a tiny
// budget, and no memory — the budget applies to a stateless run too.
func compactingAgentConfig(budget int) types.BlockConfig {
	return types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "work", Name: "worker",
		Tools:            []types.ToolConfig{toolBranch("lookup", "looks things up", nil)},
		ContextMaxTokens: budget,
		MaxIterations:    4,
	}
}

// The point of measuring: the agent compacts before sending the turn that would
// have overflowed, rather than discovering it from a rejected request.
//
// Nothing here is long enough for a chars/4 estimate to worry about — the whole
// conversation is a few hundred characters. Only the provider's reported prompt
// says it is over budget, which is what makes this a test of measurement rather
// than of counting.
func TestAgentCompactsMidLoop(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	args := func(n int) string { return `{"q":"` + strings.Repeat("a", n) + `"}` }

	fake := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(toolCallResp("lookup", args(200)), &core.LLMUsage{PromptTokens: 200, OutputTokens: 10}),
		withUsage(toolCallResp("lookup", args(240)), &core.LLMUsage{PromptTokens: 400, OutputTokens: 10}),
		// The turn that blows the budget: the next request has to be compacted.
		withUsage(toolCallResp("lookup", args(280)), &core.LLMUsage{PromptTokens: 5000, OutputTokens: 10}),
		withUsage(endTurnResp("done"), &core.LLMUsage{PromptTokens: 900, OutputTokens: 5}),
	}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), compactingAgentConfig(1000)).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fake.calls) != 4 {
		t.Fatalf("model called %d times, want 4", len(fake.calls))
	}

	// Every turn adds two messages, so the fourth request being no longer than the
	// third is only possible if the middle was dropped.
	third, fourth := fake.calls[2].Messages, fake.calls[3].Messages
	if len(fourth) >= len(third)+2 {
		t.Errorf("fourth request carried %d messages against %d: nothing was compacted",
			len(fourth), len(third))
	}
	if len(fourth) == 0 {
		t.Fatal("compaction emptied the conversation")
	}
	// A conversation has to open on a user turn, and the trailing tool results
	// cannot be separated from the assistant turn that asked for them.
	if fourth[0].Role != core.LLMRoleUser {
		t.Errorf("compacted conversation opens on %s, want a user turn", fourth[0].Role)
	}
	if last := fourth[len(fourth)-1]; last.Role != core.LLMRoleTool {
		t.Errorf("last message = %s, want the tool results the turn is answering", last.Role)
	}
	if len(fourth) < 2 || len(fourth[len(fourth)-2].ToolCalls) == 0 {
		t.Errorf("the tool results lost the assistant turn that asked for them: %+v", fourth)
	}
}

// Under budget, nothing is touched — compaction is a response to a measurement,
// not something that happens on a schedule.
func TestAgentDoesNotCompactUnderBudget(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(toolCallResp("lookup", `{"q":"x"}`), &core.LLMUsage{PromptTokens: 100, OutputTokens: 10}),
		withUsage(endTurnResp("done"), &core.LLMUsage{PromptTokens: 120, OutputTokens: 5}),
	}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), compactingAgentConfig(100000)).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(fake.calls) < 2 {
		t.Fatalf("model called %d times, want 2", len(fake.calls))
	}
	if len(fake.calls[1].Messages) <= len(fake.calls[0].Messages) {
		t.Errorf("the conversation did not grow: %d then %d messages",
			len(fake.calls[0].Messages), len(fake.calls[1].Messages))
	}
}

// Before the first measurement there is no overhead figure, so compacting would
// be guesswork against the raw estimate — and the first turn is never the one
// that overflows anyway.
func TestFitContextWaitsForAMeasurement(t *testing.T) {
	a := &aiAgent{contextMaxTokens: 1}
	msgs := []core.LLMMessage{{Role: core.LLMRoleUser, Text: strings.Repeat("a", 4000)}}
	if got := a.fitContext(context.Background(), nil, msgs, 0, newContextMeter()); len(got) != len(msgs) {
		t.Errorf("fitContext compacted before any turn was measured: %d messages", len(got))
	}
}
