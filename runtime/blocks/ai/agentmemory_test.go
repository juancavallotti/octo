package ai

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
		Type:     "ai-agent",
		Settings: types.Settings{"connector": "claude", "prompt": "chat", "tools": []map[string]any{toolBranch("noop", "does nothing", nil)}, "memoryThreadId": threadIDExpr}}
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
	stored, err := loadMemory(ctx, core.NamespaceUser, "t1")
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
		Type:     "ai-agent",
		Settings: types.Settings{"connector": "claude", "prompt": "chat", "tools": []map[string]any{toolBranch("noop", "does nothing", nil)}}}
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("ok")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).
		Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("stateless agent should not require services: %v", err)
	}
}

func TestAIAgentMemoryRejectsBadCompaction(t *testing.T) {
	cfg := memoryAgentConfig(`"t1"`)
	cfg.Settings["memoryCompaction"] = "nope"
	if _, err := tryBuildBlock(testRegistry(), depsLLM(&scriptedLLM{}), cfg); err == nil {
		t.Error("expected an error for an unknown memoryCompaction value")
	}
}

func TestClearAgentMemory(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	seed := memoryEnvelope{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}}
	if err := saveMemory(ctx, core.NamespaceUser, "t1", seed); err != nil {
		t.Fatalf("seed memory: %v", err)
	}

	clearBlock, err := newClearAgentMemory(types.Settings{"threadId": `"t1"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newClearAgentMemory: %v", err)
	}
	if _, err := clearBlock.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("clear Process: %v", err)
	}
	if got, _ := loadMemory(ctx, core.NamespaceUser, "t1"); got.Messages != nil {
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

// The overhead is the system prompt and the tool schemas: constant, and paid
// whatever the transcript is. Charging it to the tail and then asking whether the
// tail fits *half* the budget prices every turn out at once — the loop runs to the
// end, the summary replaces the whole conversation, and the turn the person is
// waiting on goes into it. A big tool set with a modest contextMaxTokens is all it
// takes, which is a configuration, not a bug in the conversation.
func TestSummarizeMemoryKeepsATailWhenTheOverheadIsLarge(t *testing.T) {
	msgs := []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: strings.Repeat("a", 400)},
		{Role: core.LLMRoleAssistant, Text: strings.Repeat("b", 400)},
		{Role: core.LLMRoleUser, Text: "the latest thing anybody said"},
	}
	meter := newContextMeter()
	// Measured at more than half the budget below, and on its own.
	meter.seed(0, 0)
	meter.observe(estimateTokens(msgs), estimateTokens(msgs)+400)

	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("SUMMARY")}}
	out := summarizeMemory(context.Background(), bareCaller(fake), mustMessage(t), msgs, 500, meter)

	if len(out) < 2 {
		t.Fatalf("the summary replaced the whole conversation: %d messages", len(out))
	}
	if last := out[len(out)-1]; !strings.Contains(last.Text, "the latest thing") {
		t.Errorf("the most recent turn was folded into the summary: last is %q", last.Text)
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
	if err := saveMemory(ctx, core.NamespaceUser, "t1", want); err != nil {
		t.Fatalf("saveMemory: %v", err)
	}
	got, err := loadMemory(ctx, core.NamespaceUser, "t1")
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

	got, err := loadMemory(ctx, core.NamespaceUser, "t1")
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
	cfg.Settings["contextMaxTokens"] = 100000

	var seen []any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(endTurnResp(strings.Repeat("a", 400)),
			&core.LLMUsage{PromptTokens: 50000, OutputTokens: 4}),
	}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	stored, err := loadMemory(ctx, core.NamespaceUser, "t1")
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
	cfg.Settings["memoryMaxTokens"] = 4000

	_, err := tryBuildBlock(testRegistry(), depsLLM(&scriptedLLM{}), cfg)
	if err == nil {
		t.Fatal("expected an error for the old memoryMaxTokens key")
	}
	if !strings.Contains(err.Error(), "contextMaxTokens") {
		t.Errorf("error = %q, want it to name the replacement", err)
	}
}

// runScopeOf is the address an agent was built at, which is half of what its tool
// scopes are composed from. Read off the built block rather than spelled out,
// because the builder mints it and a literal here would drift.
func runScopeOf(t *testing.T, p core.MessageProcessor) string {
	t.Helper()
	agent, ok := p.(*aiAgent)
	if !ok {
		t.Fatalf("not an ai-agent: %T", p)
	}
	return agent.runScope
}

// captureVars is a leaf that answers with the caller-minted variables its branch
// was handed. The agent test registry has no set-payload, and a canned result
// would not show what the runtime put on the message.
func captureVars(seen *[]any) *core.BlockRegistry {
	reg := agentRegistry(seen)
	reg.MustRegister("capture-vars", func(_ types.Settings, _ core.BlockDeps) (core.MessageProcessor, error) {
		return processorFunc(func(_ context.Context, msg *types.Message) (*types.Message, error) {
			out := map[string]any{}
			for _, name := range []string{varToolScope, varToolName, varToolCallID} {
				if v, ok := msg.Variables.String(name); ok {
					out[name] = v
				}
			}
			msg.SetBody(out)
			return msg, nil
		}), nil
	})
	return reg
}

// toolResultFor digs the content a tool branch returned out of the transcript the
// model was sent next.
//
// The LAST match, not the first. A transcript can open with turns loaded from
// memory — and when two agents share a thread it can open with ANOTHER agent's,
// which is the behaviour agent-memory.mdx warns about — so the first match is not
// necessarily from the run under test.
func toolResultFor(calls []core.LLMRequest, tool string) string {
	latest := ""
	for _, call := range calls {
		for _, m := range call.Messages {
			for _, res := range m.ToolResults {
				if res.Tool == tool {
					latest = res.Content
				}
			}
		}
	}
	return latest
}

// What a tool branch is told about the call it is running inside. Nothing here
// knows what a branch contains — an object-write, a rest call wanting an
// idempotency key, or another ai-agent all read the same three variables.
func TestAToolBranchIsToldAboutItsCall(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any

	cfg := memoryAgentConfig(`"t1"`)
	cfg.Settings["tools"] = []map[string]any{{"name": "delegate", "description": "hand it over", "process": []types.BlockConfig{{Type: "capture-vars"}}}}

	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("delegate", `{}`),
		endTurnResp("done"),
	}}
	if _, err := mustBuildAI(t, captureVars(&seen), depsLLM(fake), cfg).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := toolResultFor(fake.calls, "delegate")
	for _, want := range []string{`"toolName":"delegate"`, `"toolCallId":"call_delegate"`} {
		if !strings.Contains(strings.ReplaceAll(got, " ", ""), want) {
			t.Errorf("tool branch saw %s, want it to carry %s", got, want)
		}
	}
	// The scope opens with the conversation and ends with the tool, with the
	// calling block between them.
	compact := strings.ReplaceAll(got, " ", "")
	if !strings.Contains(compact, `"toolScope":"t1/`) || !strings.Contains(compact, `/delegate"`) {
		t.Errorf("tool branch saw %s, want a scope of the conversation, the caller and the tool", got)
	}
}

// A stateless caller has no conversation to mint from and mints a per-run id
// instead, so a branch's state is still its own and still lasts the run.
func TestAStatelessCallerStillMintsAScope(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any

	cfg := memoryAgentConfig("") // no memoryThreadId
	cfg.Settings["tools"] = []map[string]any{{"name": "delegate", "description": "hand it over", "process": []types.BlockConfig{{Type: "capture-vars"}}}}
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("delegate", `{}`),
		endTurnResp("done"),
	}}
	if _, err := mustBuildAI(t, captureVars(&seen), depsLLM(fake), cfg).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("run: %v", err)
	}

	got := toolResultFor(fake.calls, "delegate")
	// A stateless caller still mints a scope — from a per-run id rather than a
	// conversation — so a branch always has somewhere of its own.
	if !strings.Contains(got, `"toolScope":"run-`) {
		t.Errorf("tool branch saw %s, want a minted per-run scope", got)
	}
	if !strings.Contains(strings.ReplaceAll(got, " ", ""), `"toolName":"delegate"`) {
		t.Errorf("tool branch saw %s, want the tool name regardless", got)
	}
}

// The flag picks the tier and nothing else: same key, different store, and the
// persistent one is left untouched so the two cannot be confused for each other.
func TestVolatileMemoryStaysOutOfThePersistentTier(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any

	cfg := memoryAgentConfig(`"t1"`)
	cfg.Settings["memoryVolatile"] = true
	fake := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("scaffolding")}}
	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).
		Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("run: %v", err)
	}

	volatile, err := loadMemory(ctx, core.NamespaceUserVolatile, "t1")
	if err != nil {
		t.Fatalf("loadMemory (volatile): %v", err)
	}
	if !hasAssistantText(volatile.Messages, "scaffolding") {
		t.Errorf("the transcript is not in the volatile tier: %+v", volatile.Messages)
	}
	persistent, err := loadMemory(ctx, core.NamespaceUser, "t1")
	if err != nil {
		t.Fatalf("loadMemory (persistent): %v", err)
	}
	if len(persistent.Messages) != 0 {
		t.Errorf("a volatile agent wrote into the persistent tier: %+v", persistent.Messages)
	}
}

// A clear reaches both tiers. An author who flipped memoryVolatile should not
// have to say so twice, and a clear that missed would report success while
// leaving the conversation intact.
func TestClearAgentMemoryReachesTheVolatileTier(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	seed := memoryEnvelope{Messages: []core.LLMMessage{{Role: core.LLMRoleUser, Text: "hi"}}}
	for _, ns := range []string{core.NamespaceUser, core.NamespaceUserVolatile} {
		if err := saveMemory(ctx, ns, "t1", seed); err != nil {
			t.Fatalf("seed %s: %v", ns, err)
		}
	}

	clearBlock, err := newClearAgentMemory(types.Settings{"threadId": `"t1"`}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("newClearAgentMemory: %v", err)
	}
	if _, err := clearBlock.Process(ctx, mustMessage(t)); err != nil {
		t.Fatalf("clear: %v", err)
	}
	for _, ns := range []string{core.NamespaceUser, core.NamespaceUserVolatile} {
		if got, _ := loadMemory(ctx, ns, "t1"); got.Messages != nil {
			t.Errorf("%s still holds a transcript: %+v", ns, got.Messages)
		}
	}
}

// The whole point, end to end: a coordinator delegates twice in one conversation
// and the specialist it delegates to remembers the first delegation when it
// answers the second — while the coordinator's own transcript stays its own.
//
// Neither block carries anything for the case beyond the specialist saying where
// its memory lives: `memoryThreadId: vars.toolScope` is the scope the runtime
// minted for that branch, and memoryVolatile keeps a transcript that is
// scaffolding out of the store the platform backs up.
func TestASpecialistKeepsItsOwnConversationAcrossDelegations(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any

	specialist := types.BlockConfig{
		Type: "ai-agent", Name: "specialist",
		Settings: types.Settings{"connector": "claude", "answer": "text", "prompt": "one job", "memoryThreadId": "vars.toolScope", "memoryVolatile": true, "tools": []map[string]any{toolBranch("noop", "does nothing", nil)}}}
	coordinator := types.BlockConfig{
		Type: "ai-agent", Name: "coordinator",
		Settings: types.Settings{"connector": "claude", "prompt": "delegate", "memoryThreadId": `"t1"`, "tools": []map[string]any{{"name": "delegate", "description": "hand it over", "process": []types.BlockConfig{specialist}}}}}

	// Two delegations, then the coordinator answers. The specialist's two runs sit
	// inside the coordinator's one.
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp("delegate", `{"task":"first"}`),
		endTurnResp("specialist first"),
		toolCallResp("delegate", `{"task":"second"}`),
		endTurnResp("specialist second"),
		endTurnResp("coordinator answer"),
	}}

	block := mustBuildAIAt(t, agentRegistry(&seen), depsLLM(fake), coordinator, "chat")
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("run: %v", err)
	}

	// The specialist's second run opened with its own first exchange already in
	// the transcript — the delegation before it, not the coordinator's turns.
	// containsUserText compares whole turns, and an opening turn is the model's
	// arguments wrapped in a sentence, so the match is on the substring.
	var sawFirst bool
	for _, call := range fake.calls {
		if !hasAssistantText(call.Messages, "specialist first") {
			continue
		}
		for _, m := range call.Messages {
			if m.Role == core.LLMRoleUser && strings.Contains(m.Text, `"task":"second"`) {
				sawFirst = true
			}
		}
	}
	if !sawFirst {
		t.Error("the specialist did not remember its first delegation on the second")
	}

	// And it is the specialist's conversation, not the coordinator's: a volatile
	// transcript under the minted scope, with nothing of the coordinator's in it.
	// The scope is composed by the runtime, so the test asks it rather than
	// spelling out a key that would drift the moment the composition changed.
	scope := branchScopeBase("t1", runScopeOf(t, block)) + "/delegate"
	nested, err := loadMemory(ctx, core.NamespaceUserVolatile, scope)
	if err != nil {
		t.Fatalf("loadMemory (specialist): %v", err)
	}
	if !hasAssistantText(nested.Messages, "specialist first") ||
		!hasAssistantText(nested.Messages, "specialist second") {
		t.Errorf("the specialist's transcript is missing its own turns: %+v", nested.Messages)
	}
	if hasAssistantText(nested.Messages, "coordinator answer") {
		t.Error("the coordinator's answer reached the specialist's transcript")
	}

	// The coordinator's own conversation is persistent, under its own thread, and
	// carries none of the specialist's turns.
	caller, err := loadMemory(ctx, core.NamespaceUser, "t1")
	if err != nil {
		t.Fatalf("loadMemory (coordinator): %v", err)
	}
	if !hasAssistantText(caller.Messages, "coordinator answer") {
		t.Errorf("the coordinator lost its own conversation: %+v", caller.Messages)
	}
	if hasAssistantText(caller.Messages, "specialist first") {
		t.Error("the specialist wrote into the conversation a person is having")
	}
	// Nothing of the specialist's landed in the persistent tier at all.
	if stray, _ := loadMemory(ctx, core.NamespaceUser, scope); len(stray.Messages) != 0 {
		t.Errorf("a volatile specialist wrote into the persistent tier: %+v", stray.Messages)
	}
}

// Two agents on one thread is a misconfiguration the runtime warns about, and it
// must not quietly become a second one: identically-named tools on each of them
// would otherwise be handed the same scope, and two specialists keyed on it would
// share a transcript.
func TestTwoAgentsOnOneThreadGiveTheirToolsDifferentScopes(t *testing.T) {
	ctx, _ := withFakeServices(context.Background())
	var seen []any

	cfg := memoryAgentConfig(`"t1"`)
	cfg.Settings["tools"] = []map[string]any{{"name": "delegate", "description": "hand it over", "process": []types.BlockConfig{{Type: "capture-vars"}}}}

	scopeFrom := func(path string) string {
		fake := &scriptedLLM{responses: []*core.LLMResponse{
			toolCallResp("delegate", `{}`),
			endTurnResp("done"),
		}}
		block := mustBuildAIAt(t, captureVars(&seen), depsLLM(fake), cfg, path)
		if _, err := block.Process(ctx, aiMessage(t)); err != nil {
			t.Fatalf("run at %s: %v", path, err)
		}
		return toolResultFor(fake.calls, "delegate")
	}

	first, second := scopeFrom("orders.support"), scopeFrom("orders.sales")
	if first == second {
		t.Errorf("two agents sharing thread t1 handed their delegate tool the same scope: %s", first)
	}
	for _, got := range []string{first, second} {
		if !strings.Contains(strings.ReplaceAll(got, " ", ""), `"toolScope":"t1/`) {
			t.Errorf("scope %s does not open with the conversation", got)
		}
	}
}
