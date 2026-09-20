package ai

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// barrierWait is how long a parallel probe waits for the rest of the turn's
// calls before giving up. Generous, because it is only ever waited out when the
// behaviour under test is broken: the assertion is what fails, not the clock.
const barrierWait = 5 * time.Second

// parallelProbe is a tool leaf that will not return until every call of the turn
// has arrived. Run sequentially it can never get past the first one, so a test
// built on it distinguishes real concurrency from a loop that merely finishes.
type parallelProbe struct {
	want     int
	mu       sync.Mutex
	arrived  int
	inFlight int
	peak     int
	order    []string
	release  chan struct{}
}

func newParallelProbe(want int) *parallelProbe {
	return &parallelProbe{want: want, release: make(chan struct{})}
}

// arrive records one call, notes how many are in flight beside it, and blocks
// until the turn has caught up to the barrier's width.
func (p *parallelProbe) arrive(name string) bool {
	p.mu.Lock()
	p.arrived++
	p.inFlight++
	p.peak = max(p.peak, p.inFlight)
	p.order = append(p.order, name)
	full := p.arrived == p.want
	p.mu.Unlock()
	if full {
		close(p.release)
		return true
	}
	select {
	case <-p.release:
		return true
	case <-time.After(barrierWait):
		return false
	}
}

// leave records that a call has finished, so peak counts what overlapped rather
// than what ran.
func (p *parallelProbe) leave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inFlight--
}

func (p *parallelProbe) arrivals() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.order...)
}

// widest reports the most calls this probe ever saw in flight at once.
func (p *parallelProbe) widest() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.peak
}

// probeRegistry layers a "probe" leaf onto the agent registry: it announces its
// arrival at the barrier, optionally sleeps before returning so completion order
// can be made to disagree with call order, and answers with its own name.
func probeRegistry(t *testing.T, probe *parallelProbe) *core.BlockRegistry {
	t.Helper()
	var seen []any
	reg := agentRegistry(&seen)
	reg.MustRegister("probe", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		name, _ := s.String("name")
		delayMS, _ := s.Int("delayMs")
		stop, _ := s.Bool("stop")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			if !probe.arrive(name) {
				t.Errorf("tool %q waited %s alone: the turn's calls did not run together", name, barrierWait)
			}
			defer probe.leave()
			if delayMS > 0 {
				time.Sleep(time.Duration(delayMS) * time.Millisecond)
			}
			if stop {
				msg.RequestStop()
			}
			_ = msg.SetBodyJSON([]byte(`{"tool":"` + name + `"}`))
			return msg, nil
		}), nil
	})
	return reg
}

// probeBranch builds an ai-agent tool branch running a single "probe" leaf.
func probeBranch(name string, settings types.Settings) map[string]any {
	settings["name"] = name
	return map[string]any{
		"name": name, "description": "probe " + name,
		"process": []types.BlockConfig{{Type: "probe", Settings: settings}},
	}
}

// toolCallsResp builds one assistant turn asking for several tools at once,
// which is the turn this whole feature is about.
func toolCallsResp(names ...string) *core.LLMResponse {
	calls := make([]core.LLMToolCall, len(names))
	for i, name := range names {
		calls[i] = core.LLMToolCall{ID: "call_" + name, Name: name, Input: json.RawMessage(`{}`)}
	}
	return &core.LLMResponse{
		ToolCalls:  calls,
		StopReason: core.LLMStopToolUse,
		Raw:        core.LLMMessage{Role: core.LLMRoleAssistant, ToolCalls: calls},
	}
}

func parallelAgent(tools []map[string]any, workers int) types.BlockConfig {
	return types.BlockConfig{Type: "ai-agent", Settings: types.Settings{
		"connector": "claude", "prompt": "do them all",
		"maxParallelTools": workers, "tools": tools,
	}}
}

// TestAIAgentRunsToolCallsInParallel pins the point of the knob: three calls the
// model asked for in one turn are in flight together. Each probe blocks until
// all three have arrived, so a sequential dispatcher cannot pass.
func TestAIAgentRunsToolCallsInParallel(t *testing.T) {
	probe := newParallelProbe(3)
	reg := probeRegistry(t, probe)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallsResp("a", "b", "c"),
		endTurnResp(`{"done":true}`),
	}}
	cfg := parallelAgent([]map[string]any{
		probeBranch("a", types.Settings{}),
		probeBranch("b", types.Settings{}),
		probeBranch("c", types.Settings{}),
	}, 3)

	out, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if body, ok := out.Body.(map[string]any); !ok || body["done"] != true {
		t.Errorf("final body = %#v", out.Body)
	}
	if got := len(probe.arrivals()); got != 3 {
		t.Errorf("%d tools ran, want 3", got)
	}
	if got := probe.widest(); got != 3 {
		t.Errorf("at most %d calls overlapped, want all 3", got)
	}
}

// TestAIAgentParallelToolsAreCappedByTheKnob pins that the knob is a cap and not
// a suggestion: two consumers against three calls means the third waits, which
// the barrier of two lets us see without timing anything.
func TestAIAgentParallelToolsAreCappedByTheKnob(t *testing.T) {
	probe := newParallelProbe(2)
	reg := probeRegistry(t, probe)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallsResp("a", "b", "c"),
		endTurnResp(`{"done":true}`),
	}}
	cfg := parallelAgent([]map[string]any{
		probeBranch("a", types.Settings{}),
		probeBranch("b", types.Settings{}),
		probeBranch("c", types.Settings{}),
	}, 2)

	if _, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	// All three still ran — a cap delays work, it never drops it.
	if got := len(probe.arrivals()); got != 3 {
		t.Errorf("%d tools ran, want 3", got)
	}
	if got := probe.widest(); got != 2 {
		t.Errorf("%d calls overlapped, want 2: maxParallelTools is a cap, not a suggestion", got)
	}
}

// TestAIAgentParallelResultsKeepCallOrder pins that the model is answered in the
// order it asked, whatever order the answers arrived in. The first call is also
// the slowest, so a dispatcher that appends results as they land fails this.
func TestAIAgentParallelResultsKeepCallOrder(t *testing.T) {
	probe := newParallelProbe(3)
	reg := probeRegistry(t, probe)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallsResp("a", "b", "c"),
		endTurnResp(`{"done":true}`),
	}}
	cfg := parallelAgent([]map[string]any{
		probeBranch("a", types.Settings{"delayMs": 120}),
		probeBranch("b", types.Settings{"delayMs": 60}),
		probeBranch("c", types.Settings{}),
	}, 3)

	if _, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	second := fake.calls[1].Messages
	last := second[len(second)-1]
	if last.Role != core.LLMRoleTool || len(last.ToolResults) != 3 {
		t.Fatalf("second turn did not carry three tool results: %+v", last)
	}
	for i, want := range []string{"a", "b", "c"} {
		if got := last.ToolResults[i]; got.Tool != want || got.ToolCallID != "call_"+want {
			t.Errorf("result %d = %q/%q, want the %q call: results must follow call order",
				i, got.Tool, got.ToolCallID, want)
		}
	}
}

// TestAIAgentParallelToolStopHaltsTheRun pins that a stop raised inside one of
// the concurrent branches still ends the run. The branches run on copies of the
// message, so the flag has to be carried back out deliberately — dropped, the
// agent would keep calling a model nobody is waiting for.
func TestAIAgentParallelToolStopHaltsTheRun(t *testing.T) {
	probe := newParallelProbe(2)
	reg := probeRegistry(t, probe)
	fake := &scriptedLLM{repeat: toolCallsResp("a", "halt")}
	cfg := parallelAgent([]map[string]any{
		probeBranch("a", types.Settings{}),
		probeBranch("halt", types.Settings{"stop": true}),
	}, 2)

	out, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("a stop inside a parallel tool branch must halt the agent and carry the flag out")
	}
	if len(fake.calls) != 1 {
		t.Errorf("model called %d times, want 1: the agent must not iterate after a tool branch stops", len(fake.calls))
	}
}

// TestAIAgentRejectsANegativeParallelism pins the build-time sentence. Zero is
// the unset field and means one; below that is a number nobody can mean.
func TestAIAgentRejectsANegativeParallelism(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	fake := &scriptedLLM{}
	cfg := parallelAgent([]map[string]any{toolBranch("a", "tool a", types.Settings{})}, -1)

	if _, err := tryBuildBlock(reg, depsLLM(fake), cfg); err == nil {
		t.Fatal("a negative maxParallelTools must fail the build")
	}
}
