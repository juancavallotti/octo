package engine

import (
	"context"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// steerableAgentConfig builds an agent that can be reached while it runs: one
// with a conversation, since that is what a later message joins.
func steerableAgentConfig() types.BlockConfig {
	return types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "work", Name: "worker",
		Tools:          []types.ToolConfig{toolBranch("lookup", "looks things up", nil)},
		MemoryThreadID: "body.threadId",
		Input:          "body.message",
		MaxIterations:  6,
	}
}

// reentrantRegistry layers a "tool" leaf that runs hook when the model calls it.
//
// Calling the agent's own Process from inside a tool branch is what makes these
// tests deterministic and honest at once: it is a second invocation arriving
// while the first is mid-run, on the same block instance a flow's workers share,
// and it runs synchronously so there is nothing to sleep on.
func reentrantRegistry(seen *[]any, hook func()) *core.BlockRegistry {
	reg := recordRegistry(seen)
	reg.MustRegister("tool", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, m *types.Message) (*types.Message, error) {
			hook()
			return m, nil
		}), nil
	})
	return reg
}

// steerMessage is a request carrying a follow-up for an ongoing conversation.
func steerMessage(t *testing.T, text string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := msg.SetBodyJSON([]byte(`{"threadId":"t1","message":"` + text + `"}`)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return msg
}

// A second request on a conversation already being worked on is handed to the
// run in flight and stops there. Nobody gets two answers on two streams.
func TestASecondRequestJoinsTheRunInFlight(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var second *types.Message
	var secondErr error

	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("lookup", `{"q":"x"}`),
		endTurnResp("done"),
	}}
	var block core.MessageProcessor
	once := false
	reg := reentrantRegistry(&seen, func() {
		if once {
			return
		}
		once = true
		second, secondErr = block.Process(ctx, steerMessage(t, "actually, focus on pricing"))
	})
	block = mustBuildAI(t, reg, depsLLM(fake), steerableAgentConfig())

	if _, err := block.Process(ctx, steerMessage(t, "research pricing")); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if secondErr != nil {
		t.Fatalf("second request: %v", secondErr)
	}

	// The second invocation ends where it stands, with nothing to say: the answer
	// is going to whoever is reading the run it handed the message to.
	if second == nil || !second.StopRequested() {
		t.Error("the second request did not stop, so it would have started a second run")
	}
	if body, _ := second.BodyJSON(); string(body) != "{}" {
		t.Errorf("second request body = %s, want an empty object", body)
	}
	// And the run in flight answered it.
	if len(fake.calls) < 2 {
		t.Fatalf("model called %d times, want 2", len(fake.calls))
	}
	if !containsUserText(fake.calls[1].Messages, "actually, focus on pricing") {
		t.Errorf("the handed-over message never reached the conversation: %+v", fake.calls[1].Messages)
	}
	// It must not land between an assistant turn's tool calls and their results:
	// every provider requires those to be adjacent.
	for i, m := range fake.calls[1].Messages {
		if len(m.ToolCalls) > 0 && (i+1 >= len(fake.calls[1].Messages) ||
			fake.calls[1].Messages[i+1].Role != core.LLMRoleTool) {
			t.Errorf("a message was injected between a tool call and its results: %+v", fake.calls[1].Messages)
		}
	}
	// Nothing is left behind: the id is free for the next conversation.
	if len(liveRuns.runs) != 0 {
		t.Errorf("liveRuns = %d after the run, want it released", len(liveRuns.runs))
	}
}

// stopWhen ends the run in flight rather than starting one, and the model is not
// called again.
func TestStopWhenEndsTheRunInFlight(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var stopOut *types.Message

	cfg := steerableAgentConfig()
	cfg.StopWhen = "has(body.stop)"
	fake := &scriptedLLM{repeat: toolCallResp("lookup", `{"q":"x"}`)}

	var block core.MessageProcessor
	once := false
	reg := reentrantRegistry(&seen, func() {
		if once {
			return
		}
		once = true
		msg, err := types.NewMessage("")
		if err != nil {
			t.Error(err)
			return
		}
		if err := msg.SetBodyJSON([]byte(`{"threadId":"t1","stop":true}`)); err != nil {
			t.Error(err)
			return
		}
		stopOut, err = block.Process(ctx, msg)
		if err != nil {
			t.Error(err)
		}
	})
	block = mustBuildAI(t, reg, depsLLM(fake), cfg)

	out, err := block.Process(ctx, steerMessage(t, "research pricing"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Error("the stopped run did not stop its own flow")
	}
	if stopOut == nil || !stopOut.StopRequested() {
		t.Error("the stopping request did not stop its own flow")
	}
	// One turn, one tool call, then the stop — not the six the cap allows.
	if len(fake.calls) != 1 {
		t.Errorf("model called %d times after a stop, want 1", len(fake.calls))
	}

	// The conversation is still saved, and saved replayably.
	stored, err := loadMemory(ctx, "t1")
	if err != nil {
		t.Fatalf("loadMemory: %v", err)
	}
	if len(stored.Messages) == 0 {
		t.Error("a stopped run saved nothing, losing the conversation")
	}
	if last := stored.Messages[len(stored.Messages)-1]; last.Role == core.LLMRoleAssistant &&
		len(last.ToolCalls) > 0 {
		t.Error("stored a tool call with no results: the next run would replay a rejected conversation")
	}
}

// A stop can be sent blind: a client cannot know whether the run it is stopping
// is still going, so stopping nothing is a no-op rather than an error.
func TestStopWhenNothingIsRunningIsANoOp(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any

	cfg := steerableAgentConfig()
	cfg.StopWhen = "has(body.stop)"
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("done")}}

	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := msg.SetBodyJSON([]byte(`{"threadId":"t1","stop":true}`)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	out, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).Process(ctx, msg)
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Error("a stop that found nothing did not stop its own flow")
	}
	if len(fake.calls) != 0 {
		t.Errorf("model called %d times for a stop, want 0", len(fake.calls))
	}
}

// Once nothing is in flight, the next request runs for real rather than being
// swallowed by a registry entry nobody cleaned up.
func TestARequestAfterTheRunEndsStartsItsOwn(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	fake := &scriptedLLM{repeat: endTurnResp("done")}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), steerableAgentConfig())

	for i := range 2 {
		out, err := block.Process(ctx, steerMessage(t, "question"))
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if out.StopRequested() {
			t.Errorf("run %d stopped, so it was handed to a run that had already ended", i)
		}
	}
	if len(fake.calls) != 2 {
		t.Errorf("model called %d times, want 2 — both requests should have run", len(fake.calls))
	}
}

// A stateless agent claims nothing. There is no conversation for a later
// message to join, so every run of one is its own.
func TestAStatelessAgentClaimsNothing(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	cfg := steerableAgentConfig()
	cfg.MemoryThreadID = ""

	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("done")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).
		Process(ctx, steerMessage(t, "question")); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(liveRuns.runs) != 0 {
		t.Errorf("liveRuns = %d, want none claimed", len(liveRuns.runs))
	}
}

// stopWhen on a stateless agent names no conversation, so it is a build error
// rather than a condition that silently never stops anything.
func TestStopWhenRequiresAThread(t *testing.T) {
	cfg := steerableAgentConfig()
	cfg.MemoryThreadID = ""
	cfg.StopWhen = "has(body.stop)"
	if _, err := (&builder{reg: testRegistry(), deps: depsLLM(&scriptedLLM{})}).block(cfg); err == nil {
		t.Error("expected an error for stopWhen without a memoryThreadId")
	}
}

// The handover has to resolve under one lock. A caller that found a run, let go,
// and then offered could be handing work to a run that finished in between —
// losing the message and stopping its own flow on the strength of placing it.
func TestRegistryHandoverIsAllOrNothing(t *testing.T) {
	r := &runRegistry{runs: map[string]*agentRun{}}
	first := &agentRun{}

	if handed := r.offerOrClaim("t1", "hello", first); handed {
		t.Fatal("the first run was handed to itself")
	}
	if r.runs["t1"] != first {
		t.Fatal("the first run did not claim the id")
	}

	second := &agentRun{}
	if handed := r.offerOrClaim("t1", "follow-up", second); !handed {
		t.Error("a live run refused a message")
	}
	if r.runs["t1"] != first {
		t.Error("a handed-over message displaced the run it was handed to")
	}
	if got := first.take(); len(got) != 1 || got[0] != "follow-up" {
		t.Errorf("pending = %+v, want the handed-over message", got)
	}

	// Once the first is done, the next request takes the id rather than handing to
	// something that will never read it.
	first.close()
	if handed := r.offerOrClaim("t1", "later", second); handed {
		t.Error("a finished run accepted a message")
	}
	if r.runs["t1"] != second {
		t.Error("the id did not pass to the run that took over")
	}
}

// Releasing is by identity, so a run that finished cannot evict the one that has
// already taken its place.
func TestRegistryReleaseLeavesASuccessorAlone(t *testing.T) {
	r := &runRegistry{runs: map[string]*agentRun{}}
	first, second := &agentRun{}, &agentRun{}
	r.offerOrClaim("t1", "a", first)
	first.close()
	r.offerOrClaim("t1", "b", second)

	r.release("t1", first)
	if r.runs["t1"] != second {
		t.Error("releasing a finished run evicted its successor")
	}
	r.release("t1", second)
	if len(r.runs) != 0 {
		t.Error("releasing the current run left it behind")
	}
}

// A run must not finish while holding something it accepted: the invocation that
// handed it over has already stopped its own flow expecting an answer.
func TestARunCannotFinishHoldingAMessage(t *testing.T) {
	run := &agentRun{}
	if !run.offer("one more thing") {
		t.Fatal("a live run refused a message")
	}
	if run.finish() {
		t.Error("the run ended with a message it had accepted still pending")
	}
	run.take()
	if !run.finish() {
		t.Error("the run still refused to end once the message was taken")
	}
	if run.offer("too late") {
		t.Error("a finished run accepted a message")
	}
}

// Blank text is not a message, but the run still has the conversation.
//
// Appending it would block the run from finishing and then inject nothing,
// buying a billed turn to say nothing. Reporting that the run did not take it
// would be worse: the registry would evict a live run and let a rival start on
// the same thread, which is the exact thing the claim exists to stop.
func TestBlankTextIsTakenButNotInjected(t *testing.T) {
	run := &agentRun{}
	for _, text := range []string{"", "   ", "\t\n"} {
		if !run.offer(text) {
			t.Errorf("a live run disowned its conversation over blank text %q", text)
		}
	}
	if got := run.take(); len(got) != 0 {
		t.Errorf("blank text was queued for injection: %+v", got)
	}
	if !run.finish() {
		t.Error("blank text blocked the run from finishing")
	}
}

// The registry must not swap a live run out because the message offered to it
// was empty. Only a finished run gives up its conversation.
func TestBlankTextDoesNotEvictALiveRun(t *testing.T) {
	r := &runRegistry{runs: map[string]*agentRun{}}
	first, second := &agentRun{}, &agentRun{}
	r.offerOrClaim("t1", "hello", first)

	if handed := r.offerOrClaim("t1", "   ", second); !handed {
		t.Error("an empty message was not handed to the live run, so a rival would start")
	}
	if r.runs["t1"] != first {
		t.Error("an empty message evicted a live run from its own conversation")
	}
	if got := first.take(); len(got) != 0 {
		t.Errorf("the empty message was queued anyway: %+v", got)
	}
}

func containsUserText(msgs []core.LLMMessage, text string) bool {
	for i := range msgs {
		if msgs[i].Role == core.LLMRoleUser && msgs[i].Text == text {
			return true
		}
	}
	return false
}

// Two agents on the same conversation expression — body.threadId is the obvious
// way for that to happen by accident — must not hand each other messages. A
// request meant for one agent being answered by another, with the caller's flow
// stopped on the strength of it, is the kind of wrong that looks like a delivery.
func TestTwoAgentsSharingAThreadExpressionDoNotCollide(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var otherOut *types.Message

	cfg := steerableAgentConfig()

	// A second agent, identically keyed, standing in for another flow's.
	otherFake := &scriptedLLM{repeat: endTurnResp("other answer")}
	other := mustBuildAIAt(t, agentRegistry(&seen), depsLLM(otherFake), cfg, "orders.other")

	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("lookup", `{"q":"x"}`),
		endTurnResp("done"),
	}}
	reg := reentrantRegistry(&seen, func() {
		msg, err := types.NewMessage("")
		if err != nil {
			t.Error(err)
			return
		}
		if err := msg.SetBodyJSON([]byte(`{"threadId":"t1","message":"for the other agent"}`)); err != nil {
			t.Error(err)
			return
		}
		otherOut, err = other.Process(ctx, msg)
		if err != nil {
			t.Error(err)
		}
	})
	mine := mustBuildAIAt(t, reg, depsLLM(fake), cfg, "orders.mine")

	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := msg.SetBodyJSON([]byte(`{"threadId":"t1","message":"for me"}`)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	if _, err := mine.Process(ctx, msg); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The other agent ran its own conversation rather than being swallowed.
	if otherOut == nil || otherOut.StopRequested() {
		t.Error("the second agent's request was handed to the first agent's run")
	}
	if len(otherFake.calls) == 0 {
		t.Error("the second agent never called its model, so its request was taken")
	}
	// And nothing of the other agent's leaked into this one's conversation.
	for _, call := range fake.calls {
		if containsUserText(call.Messages, "for the other agent") {
			t.Errorf("another agent's message reached this conversation: %+v", call.Messages)
		}
	}
}
