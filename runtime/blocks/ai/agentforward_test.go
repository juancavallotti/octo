package ai

import (
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
)

// The whole point of forwardContext is that the value comes off the message in
// flight rather than out of configuration, so a key can reach the store without
// the runtime holding one. This checks it resolves from the inbound message and
// arrives on the calls that read and write.
func TestForwardContextReachesTheStoreFromTheInboundMessage(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Settings["forwardContext"] = `{"key": vars.dataKey, "tenant": "acme"}`

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("done")}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)

	ctx, mem, _ := withFakeMemory(t.Context())
	msg := aiMessage(t)
	msg.Variables.Set("dataKey", "from-the-message")
	if _, err := block.Process(ctx, msg); err != nil {
		t.Fatalf("process: %v", err)
	}

	for _, op := range []string{"LoadWorking", "SaveWorking", "AppendTurns"} {
		forwarded := mem.ForwardedContext(op)
		if forwarded["key"] != "from-the-message" {
			t.Errorf("%s got key %q, want the value off the message", op, forwarded["key"])
		}
		if forwarded["tenant"] != "acme" {
			t.Errorf("%s got tenant %q, want %q", op, forwarded["tenant"], "acme")
		}
	}
}

// An agent that forwards nothing must send nothing, so a store cannot tell the
// difference between this block and the one that existed before the setting did.
func TestNoForwardContextForwardsNothing(t *testing.T) {
	mem, _, _ := runStoreAgent(t, storeAgentConfig("support"), endTurnResp("done"))
	if forwarded := mem.ForwardedContext("SaveWorking"); forwarded != nil {
		t.Errorf("SaveWorking carried %v, want nothing forwarded", forwarded)
	}
}

// A forwardContext that does not resolve fails the run. Not knowing the user
// costs user memory and is logged; not resolving what the flow asked to forward
// may cost whatever the memory service needed it for, so the run does not go on
// to write under conditions the author did not ask for.
func TestForwardContextThatDoesNotResolveFailsTheRun(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Settings["forwardContext"] = `"not a map"`

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("done")}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)

	ctx, mem, _ := withFakeMemory(t.Context())
	_, err := block.Process(ctx, aiMessage(t))
	if err == nil || !strings.Contains(err.Error(), "forwardContext") {
		t.Fatalf("want a forwardContext error, got %v", err)
	}
	if _, ok, _ := mem.LoadWorking(ctx, core.MemoryRef{
		AgentID: "support", ThreadKey: "thread-1",
	}); ok {
		t.Error("a run that could not resolve its forwarded context should not have written")
	}
}

// forwardContext without an agentId would compile, evaluate, and reach nothing.
// Naming it is the same call the other memory settings make.
func TestForwardContextRequiresAnAgentID(t *testing.T) {
	cfg := memoryAgentConfig(`"thread-1"`)
	cfg.Settings["forwardContext"] = `{"key": "k"}`

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("x")}}
	_, err := tryBuildBlock(agentRegistry(&seen), depsLLM(conn), cfg)
	if err == nil || !strings.Contains(err.Error(), "forwardContext requires an agentId") {
		t.Fatalf("want a forwardContext build error, got %v", err)
	}
}

func TestForwardContextThatDoesNotCompileIsABuildError(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Settings["forwardContext"] = `{"key": vars.}`

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("x")}}
	_, err := tryBuildBlock(agentRegistry(&seen), depsLLM(conn), cfg)
	if err == nil || !strings.Contains(err.Error(), "forwardContext") {
		t.Fatalf("want a compile error naming the setting, got %v", err)
	}
}
