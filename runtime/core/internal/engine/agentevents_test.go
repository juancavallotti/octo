package engine

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// eventsRegistry layers a "record-event" leaf onto agentRegistry. It captures each
// event body, can set a variable (to prove the path cannot reach the run), and can
// ask the run to stop when a given event type arrives.
func eventsRegistry(seen *[]any, events *[]map[string]any) *core.BlockRegistry {
	reg := agentRegistry(seen)
	reg.MustRegister("record-event", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		stopOn, _ := s.String("stopOn")
		setVar, hasVar := s.String("setvar")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			body, _ := msg.Body.(map[string]any)
			*events = append(*events, body)
			if hasVar {
				msg.Variables.Set(setVar, true)
			}
			if stopOn != "" && body["type"] == stopOn {
				msg.RequestStop()
			}
			return msg, nil
		}), nil
	})
	return reg
}

// eventsPath builds an events sub-flow of a single record-event leaf.
func eventsPath(settings types.Settings) types.FlowConfig {
	return types.FlowConfig{Process: []types.BlockConfig{{Type: "record-event", Settings: settings}}}
}

// agentWithEvents builds an ai-agent with one "look" tool and an events path.
func agentWithEvents(events *types.FlowConfig, emit []string, stream bool) types.BlockConfig {
	return types.BlockConfig{
		Type:      "ai-agent",
		Connector: "claude",
		Prompt:    "do the task",
		Tools:     []types.ToolConfig{toolBranch("look", "look something up", types.Settings{})},
		Events:    events,
		Emit:      emit,
		Stream:    stream,
	}
}

// eventTypes reduces recorded events to their type sequence.
func eventTypes(events []map[string]any) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		kind, _ := e["type"].(string)
		out = append(out, kind)
	}
	return out
}

// streamingLLM is a scriptedLLM that also streams. Each turn may carry the events
// the provider would have produced on the way to that turn's response.
type streamingLLM struct {
	scriptedLLM
	deltas   [][]core.LLMStreamEvent
	streamed int
}

func (s *streamingLLM) Stream(
	ctx context.Context, req core.LLMRequest, on func(core.LLMStreamEvent) error,
) (*core.LLMResponse, error) {
	turn := s.streamed
	s.streamed++
	if turn < len(s.deltas) {
		for _, ev := range s.deltas[turn] {
			if err := on(ev); err != nil {
				return nil, err
			}
		}
	}
	return s.Complete(ctx, req)
}

func TestAIAgentEventsReportsLifecycle(t *testing.T) {
	var seen []any
	var events []map[string]any
	llm := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("look", `{"q":"refund"}`),
		endTurnResp(`{"answer":"refunded"}`),
	}}
	path := eventsPath(nil)
	block := mustBuildAI(t, eventsRegistry(&seen, &events), depsLLM(llm), agentWithEvents(&path, nil, false))

	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	// turn_end lands before the tool calls because the model's turn is what ended:
	// it stopped to ask for tools, which octo then runs.
	want := []string{"turn_start", "turn_end", "tool_call", "tool_result", "turn_start", "turn_end", "done"}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}

	call := events[2]
	if call["tool"] != "look" || call["toolCallId"] != "call_look" {
		t.Errorf("tool_call = %+v, want the tool and its call id", call)
	}
	// The arguments are decoded, so an expression in the path reads them as data
	// rather than re-parsing a string.
	input, ok := call["input"].(map[string]any)
	if !ok || input["q"] != "refund" {
		t.Errorf("tool_call input = %#v, want the decoded arguments", call["input"])
	}
	if events[3]["isError"] != false || events[3]["output"] == nil {
		t.Errorf("tool_result = %+v, want the branch's output and no error", events[3])
	}
	if events[0]["iteration"] != 1 || events[4]["iteration"] != 2 {
		t.Errorf("iterations = %v then %v, want 1 then 2", events[0]["iteration"], events[4]["iteration"])
	}
}

// TestAIAgentEmitExcludesKindEntirely pins that emit is a cost lever and not just a
// filter: an excluded kind must never reach the path at all, since on a token
// stream the whole point is not paying for an invocation per fragment.
func TestAIAgentEmitExcludesKindEntirely(t *testing.T) {
	var seen []any
	var events []map[string]any
	llm := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("look", `{"q":"refund"}`),
		endTurnResp(`{"answer":"refunded"}`),
	}}
	path := eventsPath(nil)
	cfg := agentWithEvents(&path, []string{"tool_call", "done"}, false)
	block := mustBuildAI(t, eventsRegistry(&seen, &events), depsLLM(llm), cfg)

	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	want := []string{"tool_call", "done"}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Errorf("event types = %v, want only the emitted kinds %v", got, want)
	}
}

// TestAIAgentEventsCannotReachTheRun pins that the path observes and nothing more.
// It runs on its own message, so neither the variables it sets nor the body it
// leaves behind can reach the agent's own message.
func TestAIAgentEventsCannotReachTheRun(t *testing.T) {
	var seen []any
	var events []map[string]any
	llm := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp(`{"answer":"done"}`)}}
	path := eventsPath(types.Settings{"setvar": "touchedByEvents"})
	block := mustBuildAI(t, eventsRegistry(&seen, &events), depsLLM(llm), agentWithEvents(&path, nil, false))

	out, err := block.Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("the events path never ran")
	}
	if _, touched := out.Variables["touchedByEvents"]; touched {
		t.Error("a variable set in the events path reached the agent's message")
	}
	body, ok := out.Body.(map[string]any)
	if !ok || body["answer"] != "done" {
		t.Errorf("body = %#v, want the model's answer, not an event", out.Body)
	}
}

// TestAIAgentEventsStopHaltsTheRun pins the one thing that does travel back. A stop
// means nobody is listening — an sse-event whose caller hung up is the ordinary
// cause — so the agent stops rather than spending a full run reporting to no one.
func TestAIAgentEventsStopHaltsTheRun(t *testing.T) {
	var seen []any
	var events []map[string]any
	llm := &scriptedLLM{repeat: toolCallResp("look", `{"q":"refund"}`)}
	path := eventsPath(types.Settings{"stopOn": "tool_result"})
	block := mustBuildAI(t, eventsRegistry(&seen, &events), depsLLM(llm), agentWithEvents(&path, nil, false))

	out, err := block.Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if !out.StopRequested() {
		t.Error("stop was not carried over to the agent's own message")
	}
	// One iteration only: the repeat script would otherwise run to the cap.
	if got := len(llm.calls); got != 1 {
		t.Errorf("model calls = %d, want 1 — the run should stop at the first tool result", got)
	}
}

func TestAIAgentStreamsDeltasIntoEvents(t *testing.T) {
	var seen []any
	var events []map[string]any
	llm := &streamingLLM{
		scriptedLLM: scriptedLLM{responses: []*core.LLMResponse{endTurnResp(`{"answer":"hi there"}`)}},
		deltas: [][]core.LLMStreamEvent{{
			{Kind: core.LLMStreamThinking, Text: "considering", Index: 0},
			{Kind: core.LLMStreamText, Text: "hi ", Index: 1},
			{Kind: core.LLMStreamText, Text: "there", Index: 1},
			{Kind: core.LLMStreamCustom, Name: "refusal", Text: "no", Index: 2},
		}},
	}
	path := eventsPath(nil)
	block := mustBuildAI(t, eventsRegistry(&seen, &events), depsLLM(llm), agentWithEvents(&path, nil, true))

	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	want := []string{"turn_start", "thinking", "text", "text", "custom", "turn_end", "done"}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}
	if events[2]["text"] != "hi " || events[2]["index"] != 1 {
		t.Errorf("text event = %+v, want the fragment and its block index", events[2])
	}
	if events[4]["name"] != "refusal" {
		t.Errorf("custom event = %+v, want the provider's own name for it", events[4])
	}
}

// TestAIAgentStreamStopsMidTurn pins that a stop during a stream abandons the turn
// rather than reading it to the end — the point being to stop paying a provider
// for output nobody will see.
func TestAIAgentStreamStopsMidTurn(t *testing.T) {
	var seen []any
	var events []map[string]any
	llm := &streamingLLM{
		scriptedLLM: scriptedLLM{repeat: endTurnResp(`{"answer":"hi"}`)},
		deltas: [][]core.LLMStreamEvent{{
			{Kind: core.LLMStreamText, Text: "one"},
			{Kind: core.LLMStreamText, Text: "two"},
			{Kind: core.LLMStreamText, Text: "three"},
		}},
	}
	path := eventsPath(types.Settings{"stopOn": "text"})
	block := mustBuildAI(t, eventsRegistry(&seen, &events), depsLLM(llm), agentWithEvents(&path, nil, true))

	out, err := block.Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if !out.StopRequested() {
		t.Error("stop was not carried over to the agent's own message")
	}
	want := []string{"turn_start", "text"}
	if got := eventTypes(events); !reflect.DeepEqual(got, want) {
		t.Errorf("event types = %v, want %v — the stream should end at the first stop", got, want)
	}
	if llm.streamed != 1 {
		t.Errorf("streamed turns = %d, want 1", llm.streamed)
	}
}

func TestAIAgentEventsBuildValidation(t *testing.T) {
	var seen []any
	var events []map[string]any
	reg := eventsRegistry(&seen, &events)
	path := eventsPath(nil)

	cases := []struct {
		name string
		cfg  types.BlockConfig
		conn core.Connector
		want string
	}{
		{
			name: "stream without an events path",
			cfg:  agentWithEvents(nil, nil, true),
			conn: &scriptedLLM{},
			want: "requires an events path",
		},
		{
			name: "emit without an events path",
			cfg:  agentWithEvents(nil, []string{"done"}, false),
			conn: &scriptedLLM{},
			want: "requires an events path",
		},
		{
			name: "unknown emit kind",
			cfg:  agentWithEvents(&path, []string{"tokens"}, false),
			conn: &scriptedLLM{},
			want: `emit "tokens" is not an event type`,
		},
		{
			// A connector that cannot stream must fail at build. Left to run time it
			// would look like a provider that has simply gone quiet.
			name: "stream on a connector that does not stream",
			cfg:  agentWithEvents(&path, nil, true),
			conn: &scriptedLLM{},
			want: "does not stream",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: depsLLM(tc.conn)}).block(tc.cfg)
			if err == nil {
				t.Fatalf("expected a build error mentioning %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
		})
	}

	// The same config builds once the connector streams.
	if _, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: depsLLM(&streamingLLM{})}).
		block(agentWithEvents(&path, nil, true)); err != nil {
		t.Errorf("build with a streaming connector: %v", err)
	}
}

// TestStoppedTranscriptKeepsMemoryReplayable pins what a run stopped at turn_end
// leaves behind. The turn is finished and billed by then, so dropping it would
// lose something the user paid for — but only a turn that asked for no tools can
// be kept. Tool results are produced after this point and never will be now, and
// every provider requires them to follow the turn that asked; persisting a
// dangling tool call would poison the thread and fail the *next* run instead.
func TestStoppedTranscriptKeepsMemoryReplayable(t *testing.T) {
	prior := []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}

	t.Run("a finished answer is recorded", func(t *testing.T) {
		resp := endTurnResp(`{"answer":"done"}`)
		got := stoppedTranscript(prior, resp)
		if len(got) != 2 || got[1].Role != core.LLMRoleAssistant {
			t.Fatalf("transcript = %+v, want the assistant turn appended", got)
		}
	})

	t.Run("an unanswered tool call is not", func(t *testing.T) {
		resp := toolCallResp("look", `{"q":"refund"}`)
		if got := stoppedTranscript(prior, resp); len(got) != 1 {
			t.Errorf("transcript = %+v, want the dangling tool call left out", got)
		}
	})

	t.Run("no response at all is nothing to record", func(t *testing.T) {
		if got := stoppedTranscript(prior, nil); len(got) != 1 {
			t.Errorf("transcript = %+v, want it unchanged", got)
		}
	})
}
