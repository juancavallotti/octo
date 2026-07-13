package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// retryRegistry layers a "require-field" leaf onto recordRegistry. The leaf errors
// unless the message body has the required field set, so a retry test can model a
// failure the model fixes by revising the body.
func retryRegistry(seen *[]any) *core.BlockRegistry {
	reg := recordRegistry(seen)
	reg.MustRegister("require-field", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		field, _ := s.String("require")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			body, ok := msg.Body.(map[string]any)
			if !ok || body[field] == nil {
				return nil, fmt.Errorf("missing required field %q", field)
			}
			return msg, nil
		}), nil
	})
	return reg
}

// reviseResp builds a forced revise_message tool-call response.
func reviseResp(input string) *core.LLMResponse {
	return toolCallResp("revise_message", input)
}

// endTurnResp builds a final (no-tool) assistant response.
func endTurnResp(text string) *core.LLMResponse {
	return &core.LLMResponse{
		StopReason: core.LLMStopEndTurn,
		Text:       text,
		Raw:        core.LLMMessage{Role: core.LLMRoleAssistant, Text: text},
	}
}

// refusalResp builds a refusal response.
func refusalResp() *core.LLMResponse {
	return &core.LLMResponse{StopReason: core.LLMStopRefusal, Raw: core.LLMMessage{Role: core.LLMRoleAssistant}}
}

// agentRegistry layers a "tool" leaf onto recordRegistry. The tool records the
// JSON body it received into seen, optionally sets a variable (to observe shared
// accumulation), optionally fails, and optionally replaces the body with a result.
func agentRegistry(seen *[]any) *core.BlockRegistry {
	reg := recordRegistry(seen)
	reg.MustRegister("tool", func(s types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		fail, _ := s.Bool("fail")
		setVar, hasVar := s.String("setvar")
		result, hasResult := s.String("result")
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			if fail {
				return nil, errors.New("tool failed")
			}
			raw, _ := msg.BodyJSON()
			*seen = append(*seen, string(raw))
			if hasVar {
				msg.Variables.Set(setVar, true)
			}
			if hasResult {
				_ = msg.SetBodyJSON([]byte(result))
			}
			return msg, nil
		}), nil
	})
	return reg
}

// toolBranch builds an ai-agent tool branch running a single "tool" leaf.
func toolBranch(name, desc string, settings types.Settings) types.ToolConfig {
	return types.ToolConfig{
		Name:        name,
		Description: desc,
		Process:     []types.BlockConfig{{Type: "tool", Settings: settings}},
	}
}

// scriptedLLM is a core.Connector + core.LLMClient that returns a queued sequence
// of responses, recording each request. When the queue is exhausted it returns
// repeat (if set) or a bare end_turn with no tool calls.
type scriptedLLM struct {
	responses []*core.LLMResponse
	repeat    *core.LLMResponse
	i         int
	calls     []core.LLMRequest
}

func (s *scriptedLLM) Start(context.Context, types.ConnectorConfig) error { return nil }
func (s *scriptedLLM) Stop(context.Context) error                         { return nil }
func (s *scriptedLLM) Complete(_ context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	s.calls = append(s.calls, req)
	if s.i < len(s.responses) {
		r := s.responses[s.i]
		s.i++
		return r, nil
	}
	if s.repeat != nil {
		return s.repeat, nil
	}
	return &core.LLMResponse{StopReason: core.LLMStopEndTurn}, nil
}

// toolCallResp builds a single-tool-call assistant response.
func toolCallResp(name, input string) *core.LLMResponse {
	call := core.LLMToolCall{ID: "call_" + name, Name: name, Input: json.RawMessage(input)}
	return &core.LLMResponse{
		ToolCalls:  []core.LLMToolCall{call},
		StopReason: core.LLMStopToolUse,
		Raw:        core.LLMMessage{Role: core.LLMRoleAssistant, ToolCalls: []core.LLMToolCall{call}},
	}
}

func depsLLM(conn core.Connector) core.BlockDeps {
	return core.BlockDeps{Connector: func(n string) (core.Connector, bool) {
		if n == "claude" {
			return conn, true
		}
		return nil, false
	}}
}

//nolint:ireturn // a test helper that returns the built MessageProcessor interface
func mustBuildAI(t *testing.T, reg *core.BlockRegistry, deps core.BlockDeps, cfg types.BlockConfig) core.MessageProcessor {
	t.Helper()
	block, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: deps}).block(cfg)
	if err != nil {
		t.Fatalf("build %s: %v", cfg.Type, err)
	}
	return block.Processor
}

func aiMessage(t *testing.T) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := msg.SetBodyJSON([]byte(`{"subject":"refund please"}`)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return msg
}

// routerConfig builds an ai-router with billing/tech routes and an optional
// guardrail.
func routerConfig(withGuardrail bool) types.BlockConfig {
	cfg := types.BlockConfig{
		Type:      "ai-router",
		Connector: "claude",
		Prompt:    "route the ticket",
		Routes: []types.RouteConfig{
			{Name: "billing", Description: "billing and refunds", Process: tagFlow("billing").Process},
			{Name: "tech", Description: "technical issues", Process: tagFlow("tech").Process},
		},
	}
	if withGuardrail {
		def := tagFlow("guardrail")
		cfg.Default = &def
	}
	return cfg
}

func TestAIRouterSelectsRoute(t *testing.T) {
	var seen []any
	reg := recordRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("select_route", `{"route":"billing"}`),
	}}

	proc := mustBuildAI(t, reg, depsLLM(fake), routerConfig(true))
	if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(seen) != 1 || seen[0] != "billing" {
		t.Errorf("seen = %v, want [billing]", seen)
	}
}

func TestAIRouterInspectsThenSelects(t *testing.T) {
	var seen []any
	reg := recordRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("get_body", `{}`),
		toolCallResp("select_route", `{"route":"tech"}`),
	}}

	proc := mustBuildAI(t, reg, depsLLM(fake), routerConfig(true))
	if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(seen) != 1 || seen[0] != "tech" {
		t.Errorf("seen = %v, want [tech]", seen)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(fake.calls))
	}
	// The second request should carry the get_body tool result.
	second := fake.calls[1].Messages
	last := second[len(second)-1]
	if last.Role != core.LLMRoleTool || len(last.ToolResults) != 1 {
		t.Errorf("second request did not carry a tool result: %+v", last)
	}
	if !strings.Contains(last.ToolResults[0].Content, "refund please") {
		t.Errorf("get_body result missing body: %q", last.ToolResults[0].Content)
	}
}

func TestAIRouterGuardrailPaths(t *testing.T) {
	tests := []struct {
		name     string
		route    string
		guard    bool
		wantSeen []any
	}{
		{name: "explicit guardrail", route: routeGuardrailSentinel, guard: true, wantSeen: []any{"guardrail"}},
		{name: "unknown route falls to guardrail", route: "nope", guard: true, wantSeen: []any{"guardrail"}},
		{name: "no guardrail passes through", route: routeGuardrailSentinel, guard: false, wantSeen: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen []any
			reg := recordRegistry(&seen)
			fake := &scriptedLLM{responses: []*core.LLMResponse{
				toolCallResp("select_route", `{"route":"`+tt.route+`"}`),
			}}
			proc := mustBuildAI(t, reg, depsLLM(fake), routerConfig(tt.guard))
			out, err := proc.Process(context.Background(), aiMessage(t))
			if err != nil {
				t.Fatalf("process: %v", err)
			}
			if len(seen) != len(tt.wantSeen) {
				t.Fatalf("seen = %v, want %v", seen, tt.wantSeen)
			}
			for i := range tt.wantSeen {
				if seen[i] != tt.wantSeen[i] {
					t.Errorf("seen = %v, want %v", seen, tt.wantSeen)
				}
			}
			if !tt.guard && out == nil {
				t.Error("expected passthrough message, got nil")
			}
		})
	}
}

func TestAIRouterExhaustsRoundsToGuardrail(t *testing.T) {
	var seen []any
	reg := recordRegistry(&seen)
	// The model keeps inspecting and never decides.
	fake := &scriptedLLM{repeat: toolCallResp("get_body", `{}`)}

	proc := mustBuildAI(t, reg, depsLLM(fake), routerConfig(true))
	if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(seen) != 1 || seen[0] != "guardrail" {
		t.Errorf("seen = %v, want [guardrail]", seen)
	}
	if len(fake.calls) != defaultRouterRounds {
		t.Errorf("calls = %d, want %d", len(fake.calls), defaultRouterRounds)
	}
}

func TestAIRouterBuildValidation(t *testing.T) {
	reg := testRegistry()
	deps := depsLLM(&scriptedLLM{})
	build := func(cfg types.BlockConfig) error {
		_, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: deps}).block(cfg)
		return err
	}

	base := func() types.BlockConfig {
		return types.BlockConfig{Type: "ai-router", Connector: "claude", Prompt: "x",
			Routes: []types.RouteConfig{{Name: "a", Description: "d", Process: tagFlow("a").Process}}}
	}

	if err := build(types.BlockConfig{Type: "ai-router", Connector: "claude", Prompt: "x"}); err == nil {
		t.Error("expected error with no routes")
	}
	noPrompt := base()
	noPrompt.Prompt = ""
	if err := build(noPrompt); err == nil {
		t.Error("expected error with no prompt")
	}
	noConn := base()
	noConn.Connector = ""
	if err := build(noConn); err == nil {
		t.Error("expected error with no connector")
	}
	noDesc := base()
	noDesc.Routes[0].Description = ""
	if err := build(noDesc); err == nil {
		t.Error("expected error with a route missing a description")
	}
	dup := base()
	dup.Routes = append(dup.Routes, types.RouteConfig{Name: "a", Description: "d2", Process: tagFlow("a2").Process})
	if err := build(dup); err == nil {
		t.Error("expected error with duplicate route names")
	}
}

func TestAIAgentCallsToolThenFinishes(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("lookup", `{"domain":"x.com"}`),
		endTurnResp(`{"done":true}`),
	}}

	cfg := types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "enrich",
		Tools: []types.ToolConfig{toolBranch("lookup", "look up a company", types.Settings{"result": `{"found":true}`})},
	}
	out, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	// The tool received the model's arguments as its body.
	if len(seen) != 1 || seen[0] != `{"domain":"x.com"}` {
		t.Errorf("seen = %v, want [the lookup args]", seen)
	}
	// The final assistant JSON was folded into the body.
	if body, ok := out.Body.(map[string]any); !ok || body["done"] != true {
		t.Errorf("final body = %#v", out.Body)
	}
	// The tool result was fed back on the second turn.
	second := fake.calls[1].Messages
	last := second[len(second)-1]
	if last.Role != core.LLMRoleTool || !strings.Contains(last.ToolResults[0].Content, "found") {
		t.Errorf("second turn missing tool result: %+v", last)
	}
}

// TestAIAgentStopsWhenToolBranchRequestsStop pins that a stop raised inside a tool
// branch halts the agent at once. Tool branches run on the shared message, so
// without the check the agent keeps calling the model — burning tokens and
// iterations — and the next tool call overwrites the body with its arguments. The
// model here is scripted to call the tool forever, so only the stop can end the
// loop: if the agent ignored it, it would run to the iteration cap and fail.
func TestAIAgentStopsWhenToolBranchRequestsStop(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	reg.MustRegister("stop-tool", func(types.Settings, core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			msg.RequestStop()
			return msg, nil
		}), nil
	})

	fake := &scriptedLLM{repeat: toolCallResp("halt", `{"a":1}`)}
	cfg := types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "enrich",
		Tools: []types.ToolConfig{{
			Name:        "halt",
			Description: "halts the flow",
			Process:     []types.BlockConfig{{Type: "stop-tool"}},
		}},
	}

	out, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out == nil || !out.StopRequested() {
		t.Fatal("a stop inside a tool branch must halt the agent and carry the flag out")
	}
	if len(fake.calls) != 1 {
		t.Errorf("model called %d times, want 1: the agent must not iterate after a tool branch stops", len(fake.calls))
	}
}

func TestAIAgentAccumulatesVariablesAcrossTools(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("a", `{}`),
		toolCallResp("b", `{}`),
		endTurnResp(`{}`),
	}}

	cfg := types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "do both",
		Tools: []types.ToolConfig{
			toolBranch("a", "tool a", types.Settings{"setvar": "ka"}),
			toolBranch("b", "tool b", types.Settings{"setvar": "kb"}),
		},
	}
	out, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out.Variables["ka"] != true || out.Variables["kb"] != true {
		t.Errorf("variables did not accumulate across tools: %#v", out.Variables)
	}
}

func TestAIAgentBranchErrorIsFedBack(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("boom", `{}`),
		endTurnResp(`{"ok":true}`),
	}}

	cfg := types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "try",
		Tools: []types.ToolConfig{toolBranch("boom", "fails", types.Settings{"fail": true})},
	}
	out, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t))
	if err != nil {
		t.Fatalf("process should not abort on a branch error: %v", err)
	}
	if body, ok := out.Body.(map[string]any); !ok || body["ok"] != true {
		t.Errorf("final body = %#v", out.Body)
	}
	second := fake.calls[1].Messages
	last := second[len(second)-1]
	if !last.ToolResults[0].IsError {
		t.Errorf("branch error should produce an is_error result: %+v", last.ToolResults[0])
	}
}

func TestAIAgentGuardrailAndErrorOnCap(t *testing.T) {
	t.Run("cap with guardrail runs guardrail", func(t *testing.T) {
		var seen []any
		reg := agentRegistry(&seen)
		fake := &scriptedLLM{repeat: toolCallResp("noop", `{}`)}
		def := tagFlow("guardrail")
		cfg := types.BlockConfig{
			Type: "ai-agent", Connector: "claude", Prompt: "loop", MaxIterations: 3,
			Tools:   []types.ToolConfig{toolBranch("noop", "does nothing", types.Settings{})},
			Default: &def,
		}
		if _, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t)); err != nil {
			t.Fatalf("process: %v", err)
		}
		if len(seen) == 0 || seen[len(seen)-1] != "guardrail" {
			t.Errorf("guardrail did not run last: %v", seen)
		}
		if len(fake.calls) != 3 {
			t.Errorf("calls = %d, want 3", len(fake.calls))
		}
	})

	t.Run("refusal with guardrail runs guardrail", func(t *testing.T) {
		var seen []any
		reg := agentRegistry(&seen)
		fake := &scriptedLLM{responses: []*core.LLMResponse{refusalResp()}}
		def := tagFlow("guardrail")
		cfg := types.BlockConfig{
			Type: "ai-agent", Connector: "claude", Prompt: "x",
			Tools:   []types.ToolConfig{toolBranch("noop", "n", types.Settings{})},
			Default: &def,
		}
		if _, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t)); err != nil {
			t.Fatalf("process: %v", err)
		}
		if len(seen) != 1 || seen[0] != "guardrail" {
			t.Errorf("seen = %v, want [guardrail]", seen)
		}
	})

	t.Run("cap without guardrail errors", func(t *testing.T) {
		var seen []any
		reg := agentRegistry(&seen)
		fake := &scriptedLLM{repeat: toolCallResp("noop", `{}`)}
		cfg := types.BlockConfig{
			Type: "ai-agent", Connector: "claude", Prompt: "loop", MaxIterations: 2,
			Tools: []types.ToolConfig{toolBranch("noop", "n", types.Settings{})},
		}
		if _, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t)); err == nil {
			t.Error("expected an error when the cap is hit with no guardrail")
		}
	})
}

func TestAIAgentBuildValidation(t *testing.T) {
	reg := agentRegistry(&[]any{})
	deps := depsLLM(&scriptedLLM{})
	build := func(cfg types.BlockConfig) error {
		_, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: deps}).block(cfg)
		return err
	}
	base := func() types.BlockConfig {
		return types.BlockConfig{Type: "ai-agent", Connector: "claude", Prompt: "x",
			Tools: []types.ToolConfig{toolBranch("a", "d", types.Settings{})}}
	}

	if err := build(types.BlockConfig{Type: "ai-agent", Connector: "claude", Prompt: "x"}); err == nil {
		t.Error("expected error with no tools")
	}
	noPrompt := base()
	noPrompt.Prompt = ""
	if err := build(noPrompt); err == nil {
		t.Error("expected error with no prompt")
	}
	noDesc := base()
	noDesc.Tools[0].Description = ""
	if err := build(noDesc); err == nil {
		t.Error("expected error with a tool missing a description")
	}
	dup := base()
	dup.Tools = append(dup.Tools, toolBranch("a", "d2", types.Settings{}))
	if err := build(dup); err == nil {
		t.Error("expected error with duplicate tool names")
	}
	badSchema := base()
	badSchema.Tools[0].InputSchema = `{not json`
	if err := build(badSchema); err == nil {
		t.Error("expected error with an invalid inputSchema")
	}
}

// mapResources is a core.ResourceLoader backed by an in-memory map keyed by id,
// used to serve skill/template resources in tests.
type mapResources map[string]string

func (m mapResources) Load(_ context.Context, _ core.ResourceKind, id string) ([]byte, error) {
	body, ok := m[id]
	if !ok {
		return nil, core.ErrResourceNotFound
	}
	return []byte(body), nil
}

// depsLLMRes is depsLLM plus an in-memory resource loader for skill tests.
func depsLLMRes(conn core.Connector, res mapResources) core.BlockDeps {
	deps := depsLLM(conn)
	deps.Resources = res
	return deps
}

// skillAgentConfig builds an ai-agent with one tool and the given skills.
func skillAgentConfig(skills ...types.SkillConfig) types.BlockConfig {
	return types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "help the user",
		Tools:  []types.ToolConfig{toolBranch("noop", "does nothing", types.Settings{})},
		Skills: skills,
	}
}

func TestAIAgentAdvertisesSkillsAndLoads(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("load_skill", `{"name":"refunds"}`),
		endTurnResp(`{"ok":true}`),
	}}
	res := mapResources{"skills/refunds.md": "Refund policy: always refund within 30 days."}

	cfg := skillAgentConfig(types.SkillConfig{
		Name: "refunds", Description: "How to process a refund", Resource: "skills/refunds.md",
	})
	if _, err := mustBuildAI(t, reg, depsLLMRes(fake, res), cfg).Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	// The skill is advertised in the system prompt and the load_skill tool is offered.
	req := fake.calls[0]
	if !strings.Contains(req.System, "refunds: How to process a refund") {
		t.Errorf("system prompt missing skill advertisement:\n%s", req.System)
	}
	var hasLoadSkill bool
	for _, tool := range req.Tools {
		if tool.Name == skillLoadToolName {
			hasLoadSkill = true
		}
	}
	if !hasLoadSkill {
		t.Errorf("load_skill tool not offered: %+v", req.Tools)
	}

	// The rendered resource is fed back as the tool result on the second turn.
	second := fake.calls[1].Messages
	last := second[len(second)-1]
	if last.Role != core.LLMRoleTool || !strings.Contains(last.ToolResults[0].Content, "Refund policy") {
		t.Errorf("load_skill did not return the resource body: %+v", last)
	}
	if last.ToolResults[0].IsError {
		t.Errorf("load_skill result should not be an error: %+v", last.ToolResults[0])
	}
}

func TestAIAgentLoadUnknownSkillIsError(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("load_skill", `{"name":"missing"}`),
		endTurnResp(`{}`),
	}}
	res := mapResources{"skills/refunds.md": "body"}
	cfg := skillAgentConfig(types.SkillConfig{Name: "refunds", Description: "d", Resource: "skills/refunds.md"})

	if _, err := mustBuildAI(t, reg, depsLLMRes(fake, res), cfg).Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	last := fake.calls[1].Messages[len(fake.calls[1].Messages)-1]
	if !last.ToolResults[0].IsError || !strings.Contains(last.ToolResults[0].Content, "unknown skill") {
		t.Errorf("expected an unknown-skill error result: %+v", last.ToolResults[0])
	}
}

func TestAIAgentNoSkillsNoLoadTool(t *testing.T) {
	var seen []any
	reg := agentRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp(`{}`)}}
	cfg := types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "x",
		Tools: []types.ToolConfig{toolBranch("noop", "n", types.Settings{})},
	}
	if _, err := mustBuildAI(t, reg, depsLLM(fake), cfg).Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	req := fake.calls[0]
	for _, tool := range req.Tools {
		if tool.Name == skillLoadToolName {
			t.Errorf("load_skill should not be offered when no skills are configured")
		}
	}
	if strings.Contains(req.System, "load_skill") {
		t.Errorf("system prompt should not mention load_skill without skills:\n%s", req.System)
	}
}

func TestAIAgentSkillBuildValidation(t *testing.T) {
	reg := agentRegistry(&[]any{})
	deps := depsLLMRes(&scriptedLLM{}, mapResources{})
	build := func(cfg types.BlockConfig) error {
		_, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: deps}).block(cfg)
		return err
	}

	noResource := skillAgentConfig(types.SkillConfig{Name: "a", Description: "d"})
	if err := build(noResource); err == nil {
		t.Error("expected error with a skill missing a resource")
	}
	noDesc := skillAgentConfig(types.SkillConfig{Name: "a", Resource: "r"})
	if err := build(noDesc); err == nil {
		t.Error("expected error with a skill missing a description")
	}
	dup := skillAgentConfig(
		types.SkillConfig{Name: "a", Description: "d", Resource: "r"},
		types.SkillConfig{Name: "a", Description: "d2", Resource: "r2"},
	)
	if err := build(dup); err == nil {
		t.Error("expected error with duplicate skill names")
	}
	reserved := skillAgentConfig(types.SkillConfig{Name: skillLoadToolName, Description: "d", Resource: "r"})
	if err := build(reserved); err == nil {
		t.Error("expected error with a skill using the reserved load_skill name")
	}
	collide := types.BlockConfig{
		Type: "ai-agent", Connector: "claude", Prompt: "x",
		Tools:  []types.ToolConfig{toolBranch("dup", "d", types.Settings{})},
		Skills: []types.SkillConfig{{Name: "dup", Description: "d", Resource: "r"}},
	}
	if err := build(collide); err == nil {
		t.Error("expected error with a skill colliding with a tool name")
	}
}

func retryConfig(maxAttempts int, withErrorPath bool) types.BlockConfig {
	cfg := types.BlockConfig{
		Type: "ai-retry", Connector: "claude", Prompt: "fix the body", MaxAttempts: maxAttempts,
		Process: []types.BlockConfig{{Type: "require-field", Settings: types.Settings{"require": "amount"}}},
	}
	if withErrorPath {
		cfg.Error = []types.BlockConfig{{Type: "record", Settings: types.Settings{"tag": "recovered"}}}
	}
	return cfg
}

func TestAIRetryRevisesThenSucceeds(t *testing.T) {
	var seen []any
	reg := retryRegistry(&seen)
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		reviseResp(`{"body":{"amount":42},"variables":{"fixed":true}}`),
	}}

	out, err := mustBuildAI(t, reg, depsLLM(fake), retryConfig(3, false)).
		Process(context.Background(), newMessageBody(t, `{}`))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if body, ok := out.Body.(map[string]any); !ok || body["amount"] != float64(42) {
		t.Errorf("revised body not applied: %#v", out.Body)
	}
	if out.Variables["fixed"] != true {
		t.Errorf("revision variables not merged: %#v", out.Variables)
	}
	if len(fake.calls) != 1 {
		t.Errorf("calls = %d, want 1 (one revision)", len(fake.calls))
	}
	// The revise request should carry the error so the model can react.
	prompt := fake.calls[0].Messages[0].Text
	if !strings.Contains(prompt, "amount") {
		t.Errorf("revise request missing error context: %q", prompt)
	}
	// ...and the current variables, so the model can heal by setting a missing one.
	if !strings.Contains(prompt, "Current message variables") {
		t.Errorf("revise request missing variables context: %q", prompt)
	}
}

func TestAIRetryExhaustsToErrorPath(t *testing.T) {
	var seen []any
	reg := retryRegistry(&seen)
	// Every revision still lacks "amount", so the chain keeps failing.
	fake := &scriptedLLM{repeat: reviseResp(`{"body":{"other":1}}`)}

	out, err := mustBuildAI(t, reg, depsLLM(fake), retryConfig(2, true)).
		Process(context.Background(), newMessageBody(t, `{}`))
	if err != nil {
		t.Fatalf("process should recover via the error path: %v", err)
	}
	if out == nil {
		t.Fatal("expected a recovered message")
	}
	if len(seen) != 1 || seen[0] != "recovered" {
		t.Errorf("error path did not run: %v", seen)
	}
	if len(fake.calls) != 2 {
		t.Errorf("calls = %d, want 2 (maxAttempts revisions)", len(fake.calls))
	}
}

func TestAIRetryExhaustsWithoutErrorPathReturnsError(t *testing.T) {
	var seen []any
	reg := retryRegistry(&seen)
	fake := &scriptedLLM{repeat: reviseResp(`{"body":{"other":1}}`)}

	_, err := mustBuildAI(t, reg, depsLLM(fake), retryConfig(2, false)).
		Process(context.Background(), newMessageBody(t, `{}`))
	if err == nil {
		t.Error("expected the last error to propagate when no error path is configured")
	}
}

func TestAIRetryPassesThroughOnSuccess(t *testing.T) {
	var seen []any
	reg := retryRegistry(&seen)
	fake := &scriptedLLM{}

	// Body already valid: the chain succeeds on the first run, no LLM call.
	out, err := mustBuildAI(t, reg, depsLLM(fake), retryConfig(3, true)).
		Process(context.Background(), newMessageBody(t, `{"amount":7}`))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if out == nil || len(fake.calls) != 0 {
		t.Errorf("expected success with no revision; calls=%d", len(fake.calls))
	}
}

func TestAIRetryBuildValidation(t *testing.T) {
	reg := retryRegistry(&[]any{})
	deps := depsLLM(&scriptedLLM{})
	build := func(cfg types.BlockConfig) error {
		_, err := (&builder{reg: reg, pool: pool.New(0, 0), deps: deps}).block(cfg)
		return err
	}

	if err := build(types.BlockConfig{Type: "ai-retry", Connector: "claude", Prompt: "x"}); err == nil {
		t.Error("expected error with no process chain")
	}
	if err := build(types.BlockConfig{Type: "ai-retry", Connector: "claude",
		Process: []types.BlockConfig{{Type: "require-field"}}}); err == nil {
		t.Error("expected error with no prompt")
	}
	if err := build(types.BlockConfig{Type: "ai-retry", Prompt: "x",
		Process: []types.BlockConfig{{Type: "require-field"}}}); err == nil {
		t.Error("expected error with no connector")
	}
}

func newMessageBody(t *testing.T, body string) *types.Message {
	t.Helper()
	msg, err := types.NewMessage("")
	if err != nil {
		t.Fatalf("new message: %v", err)
	}
	if err := msg.SetBodyJSON([]byte(body)); err != nil {
		t.Fatalf("set body: %v", err)
	}
	return msg
}
