package ai

import (
	"context"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// gatedAgentConfig builds an agent whose one tool needs a person, with a
// conversation for the answer to arrive on and an events path to ask through.
func gatedAgentConfig(authorize string, events *types.FlowConfig) types.BlockConfig {
	tool := toolBranch("write", "changes something", types.Settings{})
	tool["authorize"] = authorize
	return types.BlockConfig{
		Type: "ai-agent", Name: "worker",
		Settings: types.Settings{"connector": "claude", "prompt": "work", "tools": []map[string]any{tool}, "memoryThreadId": "body.threadId", "input": `has(body.message) ? body.message : ""`, "authorizeId": `has(body.authorizationId) ? body.authorizationId : ""`, "authorizeAllow": `has(body.allow) && body.allow == true`, "emit": []string{eventToolAuthorization, eventToolCall, eventToolResult, eventSignal}, "events": events, "maxIterations": 4}}
}

// answeringRegistry layers an events leaf that records every event and calls
// answer when a tool authorization arrives.
//
// The answer is delivered from inside the events path, which is what makes these
// tests deterministic: the gate is opened before the event is reported, so an
// answer sent while the event is still travelling is exactly the race a real
// panel wins, and here it happens on one goroutine with nothing to sleep on.
func answeringRegistry(
	seen *[]any, events *[]map[string]any, answer func(body map[string]any),
) *core.BlockRegistry {
	reg := recordRegistry(seen)
	reg.MustRegister("tool", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, m *types.Message) (*types.Message, error) {
			*seen = append(*seen, m.Variables[varToolName])
			return m, nil
		}), nil
	})
	reg.MustRegister("record-event", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		closed, _ := s.Bool("closed")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			body, _ := msg.Body.(map[string]any)
			*events = append(*events, body)
			if body["type"] == eventToolAuthorization {
				if closed {
					// What `ifClosed: stop` does when nobody is reading the stream.
					msg.RequestStop()
					return msg, nil
				}
				if answer != nil {
					answer(body)
				}
			}
			return msg, nil
		}), nil
	})
	return reg
}

// answerMessage is a request carrying a person's decision about one call.
func answerMessage(t *testing.T, id string, allow bool) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	body := `{"threadId":"t1","authorizationId":"` + id + `","allow":`
	if allow {
		body += "true}"
	} else {
		body += "false}"
	}
	if err := msg.SetBodyJSON([]byte(body)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return msg
}

// ranTools reports whether the tool branch actually ran, which is the only thing
// a denial is finally about.
func ranTools(seen []any) int { return len(seen) }

// resultOf finds the tool results the agent sent back to the model on its second
// turn — what the model was told about the call it asked for.
func agentToolResult(t *testing.T, fake *scriptedLLM) core.LLMToolResult {
	t.Helper()
	if len(fake.calls) < 2 {
		t.Fatalf("model called %d times, want a second turn carrying the tool result", len(fake.calls))
	}
	for _, m := range fake.calls[len(fake.calls)-1].Messages {
		if len(m.ToolResults) > 0 {
			return m.ToolResults[0]
		}
	}
	t.Fatal("no tool result reached the model")
	return core.LLMToolResult{}
}

// A person allows the call, and it runs.
func TestAnAuthorizedToolCallRuns(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var events []map[string]any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("write", `{"method":"PUT"}`),
		endTurnResp("done"),
	}}

	var block core.MessageProcessor
	reg := answeringRegistry(&seen, &events, func(body map[string]any) {
		id, _ := body[fieldAuthorizationID].(string)
		if _, err := block.Process(ctx, answerMessage(t, id, true)); err != nil {
			t.Errorf("answer: %v", err)
		}
	})
	path := eventsPath(types.Settings{})
	block = mustBuildAI(t, reg, depsLLM(fake), gatedAgentConfig(`input.method != "GET"`, &path))

	if _, err := block.Process(ctx, steerMessage(t, "change it")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if ranTools(seen) != 1 {
		t.Errorf("the tool branch ran %d times, want 1: an allowed call must run", ranTools(seen))
	}
	if res := agentToolResult(t, fake); res.IsError {
		t.Errorf("the model was told the call failed: %+v", res)
	}
	// The decision is reported where the run consumed it, so a transcript shows who
	// allowed what rather than only that a tool ran.
	if !containsEvent(events, eventSignal, fieldAllowed, true) {
		t.Errorf("the authorization was not reported as a signal: %v", eventTypes(events))
	}
}

// A person denies the call. The branch never runs, and the model is told plainly.
func TestADeniedToolCallDoesNotRun(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var events []map[string]any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("write", `{"method":"PUT"}`),
		endTurnResp("understood"),
	}}

	var block core.MessageProcessor
	reg := answeringRegistry(&seen, &events, func(body map[string]any) {
		id, _ := body[fieldAuthorizationID].(string)
		if _, err := block.Process(ctx, answerMessage(t, id, false)); err != nil {
			t.Errorf("answer: %v", err)
		}
	})
	path := eventsPath(types.Settings{})
	block = mustBuildAI(t, reg, depsLLM(fake), gatedAgentConfig(`input.method != "GET"`, &path))

	if _, err := block.Process(ctx, steerMessage(t, "change it")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if ranTools(seen) != 0 {
		t.Errorf("the tool branch ran %d times, want 0: a denied call must not run", ranTools(seen))
	}
	res := agentToolResult(t, fake)
	if !res.IsError || !strings.Contains(res.Content, "denied") {
		t.Errorf("the model was not told the call was denied: %+v", res)
	}
	// And the run carries on rather than ending: the model got a turn to answer for
	// itself, which is the whole reason a denial is a tool result.
	if len(fake.calls) != 2 {
		t.Errorf("model called %d times, want 2: a denial must not end the run", len(fake.calls))
	}
}

// Nobody answers. The call is denied on their behalf rather than holding the run
// open, and the reason says so.
func TestAnUnansweredAuthorizationTimesOut(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var events []map[string]any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("write", `{"method":"PUT"}`),
		endTurnResp("nobody was there"),
	}}

	reg := answeringRegistry(&seen, &events, nil)
	path := eventsPath(types.Settings{})
	cfg := gatedAgentConfig(`input.method != "GET"`, &path)
	cfg.Settings["authorizeTimeout"] = "10ms"
	block := mustBuildAI(t, reg, depsLLM(fake), cfg)

	if _, err := block.Process(ctx, steerMessage(t, "change it")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if ranTools(seen) != 0 {
		t.Errorf("the tool branch ran %d times, want 0", ranTools(seen))
	}
	res := agentToolResult(t, fake)
	if !res.IsError || !strings.Contains(res.Content, "within 10ms") {
		t.Errorf("the model was not told nobody authorized the call: %+v", res)
	}
}

// The connection that would have asked is gone. Denying immediately is the honest
// reading of that; waiting out the clock would hold a conversation open for
// somebody who has already left.
func TestAClosedConnectionDeniesImmediately(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var events []map[string]any
	fake := &scriptedLLM{repeat: toolCallResp("write", `{"method":"PUT"}`)}

	reg := answeringRegistry(&seen, &events, nil)
	path := eventsPath(types.Settings{"closed": true})
	cfg := gatedAgentConfig(`input.method != "GET"`, &path)
	// Long enough that a test finishing quickly proves the clock was never run.
	cfg.Settings["authorizeTimeout"] = "10m"
	block := mustBuildAI(t, reg, depsLLM(fake), cfg)

	out, err := block.Process(ctx, steerMessage(t, "change it"))
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if ranTools(seen) != 0 {
		t.Errorf("the tool branch ran %d times, want 0", ranTools(seen))
	}
	if out == nil || !out.StopRequested() {
		t.Error("the run carried on after the events path stopped it")
	}
}

// The condition is about the arguments, not the tool: the same tool runs freely
// for a call that does not match and is gated for one that does.
func TestTheGateReadsTheArguments(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var events []map[string]any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("write", `{"method":"GET"}`),
		endTurnResp("read it"),
	}}

	reg := answeringRegistry(&seen, &events, nil)
	path := eventsPath(types.Settings{})
	cfg := gatedAgentConfig(`input.method != "GET"`, &path)
	cfg.Settings["authorizeTimeout"] = "10m" // never reached: nothing should wait
	block := mustBuildAI(t, reg, depsLLM(fake), cfg)

	if _, err := block.Process(ctx, steerMessage(t, "read it")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if ranTools(seen) != 1 {
		t.Errorf("the tool branch ran %d times, want 1: an ungated call must not wait", ranTools(seen))
	}
	for _, kind := range eventTypes(events) {
		if kind == eventToolAuthorization {
			t.Error("a call the condition did not match was put in front of a person")
		}
	}
}

// A tool that says nothing about authorization is free, which is what keeps a
// person's attention for the calls that need it.
func TestAToolWithoutAGateIsFree(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any
	var events []map[string]any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("write", `{"method":"PUT"}`),
		endTurnResp("done"),
	}}

	reg := answeringRegistry(&seen, &events, nil)
	path := eventsPath(types.Settings{})
	cfg := gatedAgentConfig("", &path)
	block := mustBuildAI(t, reg, depsLLM(fake), cfg)

	if _, err := block.Process(ctx, steerMessage(t, "change it")); err != nil {
		t.Fatalf("run: %v", err)
	}
	if ranTools(seen) != 1 {
		t.Errorf("the tool branch ran %d times, want 1", ranTools(seen))
	}
}

// A gate that cannot be answered only denies, so each missing half is refused
// where it can still be fixed: at build time.
func TestAnUnanswerableGateIsRefusedAtBuild(t *testing.T) {
	path := eventsPath(types.Settings{})
	cases := []struct {
		name string
		want string
		edit func(cfg *types.BlockConfig)
	}{
		{"no way to be told", "authorizeId is required", func(cfg *types.BlockConfig) {
			cfg.Settings["authorizeId"], cfg.Settings["authorizeAllow"] = "", ""
		}},
		{"no way to ask", "requires an events path", func(cfg *types.BlockConfig) {
			cfg.Settings["events"] = nil
		}},
		{"asking on a path that drops the event", "requires an events path", func(cfg *types.BlockConfig) {
			cfg.Settings["emit"] = []string{eventToolCall}
		}},
		{"no conversation to answer on", "requires memoryThreadId", func(cfg *types.BlockConfig) {
			cfg.Settings["memoryThreadId"] = ""
		}},
		{"an id with nothing to answer", "requires authorizeAllow", func(cfg *types.BlockConfig) {
			cfg.Settings["authorizeAllow"] = ""
		}},
		{"a wait that is not a duration", "authorizeTimeout", func(cfg *types.BlockConfig) {
			cfg.Settings["authorizeTimeout"] = "soon"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen []any
			var events []map[string]any
			cfg := gatedAgentConfig(`input.method != "GET"`, &path)
			tc.edit(&cfg)
			_, err := tryBuildBlock(answeringRegistry(&seen, &events, nil), depsLLM(&scriptedLLM{}), cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("build error = %v, want one mentioning %q", err, tc.want)
			}
		})
	}
}

// An answer says yes or no to a particular call, so an allow condition with no id
// beside it is a setting that cannot do anything.
func TestAnAllowConditionWithoutAnIDIsRefused(t *testing.T) {
	var seen []any
	var events []map[string]any
	path := eventsPath(types.Settings{})
	cfg := gatedAgentConfig("", &path)
	cfg.Settings["authorizeId"] = ""
	_, err := tryBuildBlock(answeringRegistry(&seen, &events, nil), depsLLM(&scriptedLLM{}), cfg)
	if err == nil || !strings.Contains(err.Error(), "authorizeAllow requires authorizeId") {
		t.Fatalf("build error = %v, want one about authorizeAllow needing an id", err)
	}
}

// An mcp-router has nowhere to ask and no run to hold the call while it waits;
// its client runs its own consent step.
func TestAnMCPRouterRefusesAToolGate(t *testing.T) {
	var seen []any
	tool := toolBranch("write", "changes something", types.Settings{})
	tool["authorize"] = "true"
	_, err := tryBuildBlock(recordRegistry(&seen), core.BlockDeps{}, types.BlockConfig{
		Type: "mcp-router", Name: "router",
		Settings: types.Settings{"tools": []map[string]any{tool}}})
	if err == nil || !strings.Contains(err.Error(), "authorize is an ai-agent setting") {
		t.Fatalf("build error = %v, want one about authorize being an ai-agent setting", err)
	}
}

// containsEvent reports whether an event of the given type carries field == want.
func containsEvent(events []map[string]any, kind, field string, want any) bool {
	for _, e := range events {
		if e["type"] == kind && e[field] == want {
			return true
		}
	}
	return false
}
