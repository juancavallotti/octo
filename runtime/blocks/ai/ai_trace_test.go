package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// turnCollector keeps the llm.turn records an agent published.
type turnCollector struct {
	mu      sync.Mutex
	records []types.TraceEvent
}

func (c *turnCollector) Enabled() bool { return true }

func (c *turnCollector) Publish(event types.TraceEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, event)
}

func (c *turnCollector) turns() []types.TraceEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var turns []types.TraceEvent
	for _, record := range c.records {
		if record.Kind == types.TraceLLMTurn {
			turns = append(turns, record)
		}
	}
	return turns
}

// tracingAgent installs a collector as the process tracer for one test.
func tracingAgent(t *testing.T) *turnCollector {
	t.Helper()
	c := &turnCollector{}
	previous := core.Tracer()
	core.SetTracer(c)
	t.Cleanup(func() { core.SetTracer(previous) })
	return c
}

// agentTurnConfig is a minimal one-tool agent, addressed so the record can be
// checked for landing at the right block.
func agentTurnConfig() types.BlockConfig {
	return types.BlockConfig{
		Type:     blockKindAIAgent,
		Name:     "triage",
		Settings: types.Settings{"connector": "claude", "prompt": "triage the message", "tools": []map[string]any{{"name": "revise_message", "description": "revise the message", "process": []types.BlockConfig{{Type: "tool"}}}}}}
}

// A traced agent records one turn per model call — including the turns that asked
// for tools, which is what makes the token accounting add up.
func TestAgentTracesEveryTurn(t *testing.T) {
	c := tracingAgent(t)

	llm := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(reviseResp(`{"subject":"refund"}`), &core.LLMUsage{InputTokens: 100, OutputTokens: 20}),
		withUsage(endTurnResp("done"), &core.LLMUsage{InputTokens: 150, OutputTokens: 5, CachedTokens: 90}),
	}}

	var seen []any
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(llm), agentTurnConfig())
	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 2 {
		t.Fatalf("llm.turn records = %d, want 2 (one per model call)", len(turns))
	}
	for i, turn := range turns {
		if got := turn.Attrs["iteration"]; got != i+1 {
			t.Errorf("turn %d iteration = %v, want %d", i, got, i+1)
		}
		if turn.BlockType != blockKindAIAgent {
			t.Errorf("turn %d BlockType = %q", i, turn.BlockType)
		}
		if got := turn.Attrs["block"]; got != "triage" {
			t.Errorf("turn %d block = %v, want triage", i, got)
		}
		if got := turn.Attrs["connector"]; got != "claude" {
			t.Errorf("turn %d connector = %v, want claude", i, got)
		}
		if turn.DurationNs < 0 {
			t.Errorf("turn %d has a negative duration", i)
		}
	}

	// The first turn asked for a tool, the second ended it — the record has to
	// distinguish them, since only one of them is the answer.
	if got := turns[0].Attrs["stopReason"]; got != string(core.LLMStopToolUse) {
		t.Errorf("first turn stopReason = %v, want %s", got, core.LLMStopToolUse)
	}
	if got := turns[0].Attrs["toolCalls"]; got != 1 {
		t.Errorf("first turn toolCalls = %v, want 1", got)
	}
	if got := turns[1].Attrs["stopReason"]; got != string(core.LLMStopEndTurn) {
		t.Errorf("second turn stopReason = %v, want %s", got, core.LLMStopEndTurn)
	}
}

// Usage is what the stacked cost work reads, so it has to arrive per turn and in
// the shape the events path already reports.
func TestAgentTraceCarriesUsageAndModel(t *testing.T) {
	c := tracingAgent(t)

	resp := withUsage(endTurnResp("done"), &core.LLMUsage{
		InputTokens: 150, OutputTokens: 25, ThinkingTokens: 10, CachedTokens: 90,
	})
	resp.Model = "claude-sonnet-4-6-20260101"

	var seen []any
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(&scriptedLLM{repeat: resp}), agentTurnConfig())
	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 1 {
		t.Fatalf("llm.turn records = %d, want 1", len(turns))
	}
	if got := turns[0].Attrs["model"]; got != "claude-sonnet-4-6-20260101" {
		t.Errorf("model = %v, want the snapshot that served the turn", got)
	}
	usage, ok := turns[0].Attrs["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage = %T, want a map", turns[0].Attrs["usage"])
	}
	for field, want := range map[string]int{
		"inputTokens": 150, "outputTokens": 25, "thinkingTokens": 10, "cachedTokens": 90,
	} {
		if usage[field] != want {
			t.Errorf("usage[%s] = %v, want %d", field, usage[field], want)
		}
	}
}

// A turn that failed is the one worth reading. Recording only the successes would
// make a trace of a failing agent look empty.
func TestAgentTracesAFailedTurn(t *testing.T) {
	c := tracingAgent(t)

	var seen []any
	llm := &failingLLM{err: errors.New("provider unavailable")}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(llm), agentTurnConfig())
	if _, err := block.Process(context.Background(), aiMessage(t)); err == nil {
		t.Fatal("a failing model produced no error")
	}

	turns := c.turns()
	if len(turns) != 1 {
		t.Fatalf("llm.turn records = %d, want 1", len(turns))
	}
	if turns[0].Err == "" {
		t.Error("the failed turn recorded no error")
	}
	if _, ok := turns[0].Attrs["usage"]; ok {
		t.Error("a failed turn reported usage")
	}
}

// With tracing off the agent must build nothing at all: this runs once per model
// turn, on every agent in the process.
func TestAgentPublishesNothingWhenTracingIsOff(t *testing.T) {
	c := tracingAgent(t)
	core.SetTracer(nil)

	var seen []any
	llm := &scriptedLLM{repeat: endTurnResp("done")}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(llm), agentTurnConfig())
	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	if turns := c.turns(); len(turns) != 0 {
		t.Fatalf("llm.turn records = %d with tracing off, want 0", len(turns))
	}
}

// withUsage attaches token accounting to a scripted response.
func withUsage(resp *core.LLMResponse, usage *core.LLMUsage) *core.LLMResponse {
	resp.Usage = usage
	return resp
}

// failingLLM is a connector whose every turn fails, standing in for a provider
// outage or a refused request.
type failingLLM struct{ err error }

func (f *failingLLM) Start(context.Context, types.ConnectorConfig) error { return nil }
func (f *failingLLM) Stop(context.Context) error                         { return nil }

func (f *failingLLM) Complete(context.Context, core.LLMRequest) (*core.LLMResponse, error) {
	return nil, f.err
}

// ofKind returns the collected records of one kind.
func (c *turnCollector) ofKind(kind types.TraceKind) []types.TraceEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []types.TraceEvent
	for _, record := range c.records {
		if record.Kind == kind {
			out = append(out, record)
		}
	}
	return out
}

// Pruning calls no model and costs nothing, so it produces no llm.turn — and
// without a record of its own it would leave no trace at all. When an agent has
// forgotten something, when it forgot is the first thing anyone needs, and it is
// exactly the thing a cost-shaped trace would not show.
func TestPruningCompactionIsTracedEvenThoughItCostsNothing(t *testing.T) {
	c := tracingAgent(t)
	ctx, _ := withFakeServices(context.Background())

	args := func(n int) string { return `{"q":"` + strings.Repeat("a", n) + `"}` }
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(toolCallResp("lookup", args(200)), &core.LLMUsage{PromptTokens: 200, OutputTokens: 10}),
		withUsage(toolCallResp("lookup", args(240)), &core.LLMUsage{PromptTokens: 400, OutputTokens: 10}),
		withUsage(toolCallResp("lookup", args(280)), &core.LLMUsage{PromptTokens: 5000, OutputTokens: 10}),
		withUsage(endTurnResp("done"), &core.LLMUsage{PromptTokens: 900, OutputTokens: 5}),
	}}
	var seen []any
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), compactingAgentConfig(1000)).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	records := c.ofKind(types.TraceAgentCompaction)
	if len(records) != 1 {
		t.Fatalf("agent.compaction records = %d, want 1", len(records))
	}
	record := records[0]
	if record.BlockType != blockKindAIAgent || record.Attrs[attrBlock] != "worker" {
		t.Errorf("record landed at %q/%v, want the agent that compacted",
			record.BlockType, record.Attrs[attrBlock])
	}
	// The reading and the budget it was measured against, so a reader does not have
	// to go and find the block's configuration to interpret the number.
	for _, attr := range []string{"strategy", "before", "after", "dropped", "contextMaxTokens"} {
		if _, ok := record.Attrs[attr]; !ok {
			t.Errorf("record carried no %q: %+v", attr, record.Attrs)
		}
	}
	if record.Attrs["strategy"] != memoryCompactPrune {
		t.Errorf("strategy = %v, want %q", record.Attrs["strategy"], memoryCompactPrune)
	}
	// It is not a turn, and must not be counted as one: pricing it as a model call
	// would invent a charge for work that made no request.
	for _, turn := range c.turns() {
		if turn.Attrs[attrPurpose] == purposeMemory {
			t.Error("a pruning compaction was recorded as a model turn")
		}
	}
}
