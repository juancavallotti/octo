package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/core/internal/pool"
	"github.com/juancavallotti/octo/runtime/types"
)

// storeAgentConfig builds a memory-enabled ai-agent that has opted into the
// first-class store by naming itself.
func storeAgentConfig(agentID string) types.BlockConfig {
	cfg := memoryAgentConfig(`"thread-1"`)
	cfg.AgentID = agentID
	return cfg
}

// runStoreAgent builds an agent over a scripted model and runs it once against a
// context carrying a real in-memory store.
func runStoreAgent(
	t *testing.T, cfg types.BlockConfig, resps ...*core.LLMResponse,
) (*fakeMemory, *fakeKV, *types.Message) {
	t.Helper()
	var seen []any
	conn := &scriptedLLM{responses: resps}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	ctx, mem, kv := withFakeMemory(context.Background())
	out, err := block.Process(ctx, aiMessage(t))
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	return mem, kv, out
}

// buildStoreAgent builds an agent and returns the build error, for the cases
// where refusing to build IS the behaviour under test.
func buildStoreAgent(t *testing.T, cfg types.BlockConfig) (core.MessageProcessor, error) {
	t.Helper()
	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("done")}}
	block, err := (&builder{
		reg: agentRegistry(&seen), pool: pool.New(0, 0), deps: depsLLM(conn),
	}).block(cfg)
	if err != nil {
		return nil, err
	}
	return block.Processor, nil
}

// TestAgentStoreRecordsCompletedTurn is the headline behaviour: an agent that
// names itself records what was asked and what it answered into durable history,
// separately from the working memory it will later compact.
func TestAgentStoreRecordsCompletedTurn(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Input = `"how do I get a refund?"`
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp("ask billing"))

	turns := mem.turnsFor("support", "thread-1")
	if len(turns) != 2 {
		t.Fatalf("want the question and the answer recorded, got %d turns", len(turns))
	}
	if turns[0].Role != core.LLMRoleUser || turns[0].Text != "how do I get a refund?" {
		t.Errorf("first turn should be the question, got %s %q", turns[0].Role, turns[0].Text)
	}
	if turns[1].Role != core.LLMRoleAssistant || turns[1].Text != "ask billing" {
		t.Errorf("second turn should be the answer, got %s %q", turns[1].Role, turns[1].Text)
	}
	if turns[0].Seq != 1 || turns[1].Seq != 2 {
		t.Errorf("the store assigns sequence numbers, got %d and %d", turns[0].Seq, turns[1].Seq)
	}
}

// TestAgentStoreTitlesNewConversation checks the one naming decision the runtime
// makes: a conversation it just opened gets a label so a list has something to
// show. A better title is a model call and belongs to the flow that wants it.
func TestAgentStoreTitlesNewConversation(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Input = `"how do I get a refund?\nsecond line is dropped"`
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp("ask billing"))

	if got := mem.titleOf("support", "thread-1"); got != "how do I get a refund?" {
		t.Errorf("title should be the first line of the opening turn, got %q", got)
	}
}

// TestAgentStoreNamesWithTheChain checks the nameThread slot: whatever it
// returns becomes the title, and the ENGINE is what writes it.
//
// That division is the point of the slot. Naming is a model call, so which model
// and which prompt belong to the flow author. Where the conversation lives is the
// engine's, so the chain never sees a thread key, never composes a route, and
// never talks to a store it is already inside — which is exactly what the queue
// and callback this replaced spent its time doing, and got wrong.
func TestAgentStoreNamesWithTheChain(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Input = `"how do I get a refund?"`
	cfg.NameThread = &types.FlowConfig{Process: []types.BlockConfig{{
		Type:     "tool",
		Settings: types.Settings{"result": `"Refund, politely asked"`},
	}}}
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp("ask billing"))

	if got := mem.titleOf("support", "thread-1"); got != "Refund, politely asked" {
		t.Errorf("the chain's answer should be the title, got %q", got)
	}
}

// TestAgentStoreNameChainCanDecline checks that an empty answer names nothing
// rather than falling back to the opening question.
//
// A chain that returned nothing has said something: this exchange is not worth a
// name. Overriding that with the first line of the question would be ignoring it,
// and the fallback exists for agents that declare no chain at all.
func TestAgentStoreNameChainCanDecline(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Input = `"how do I get a refund?"`
	cfg.NameThread = &types.FlowConfig{Process: []types.BlockConfig{{
		Type:     "tool",
		Settings: types.Settings{"result": `""`},
	}}}
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp("ask billing"))

	if got := mem.titleOf("support", "thread-1"); got != "" {
		t.Errorf("an empty answer should name nothing, got %q", got)
	}
}

// TestAgentStoreNameChainFailureFallsBack checks that a naming chain which blows
// up costs a good title and nothing else.
//
// The person already has their answer by the time this runs, so taking the run
// down over a label would be a poor trade. It falls back to the truncation rather
// than to nothing, because a failed chain has said nothing — unlike one that
// answered with an empty string, which has.
func TestAgentStoreNameChainFailureFallsBack(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Input = `"how do I get a refund?"`
	cfg.NameThread = &types.FlowConfig{Process: []types.BlockConfig{{
		Type:     "tool",
		Settings: types.Settings{"fail": true},
	}}}
	mem, _, out := runStoreAgent(t, cfg, endTurnResp("ask billing"))

	if got := mem.titleOf("support", "thread-1"); got != "how do I get a refund?" {
		t.Errorf("a failed chain should fall back to the opening turn, got %q", got)
	}
	// And the run still produced its answer.
	if out == nil {
		t.Error("a failed naming chain must not fail the run")
	}
}

// TestAgentStoreNameThreadNeedsAnAgentID checks the slot is refused without one.
//
// Without an agentId there is no first-class store, so there is nothing a title
// could be written to. A block that accepted the slot anyway would pay for a
// model call per conversation and discard every answer.
func TestAgentStoreNameThreadNeedsAnAgentID(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.AgentID = ""
	cfg.NameThread = &types.FlowConfig{Process: []types.BlockConfig{{
		Type:     "set-payload",
		Settings: types.Settings{"value": `"anything"`},
	}}}
	if _, err := buildStoreAgent(t, cfg); err == nil {
		t.Error("nameThread without an agentId should fail the build")
	}
}

// TestAgentStoreSavesWorkingMemory checks that the transcript reaches the store
// rather than the legacy KV blob once an agent has named itself.
func TestAgentStoreSavesWorkingMemory(t *testing.T) {
	cfg := storeAgentConfig("support")
	mem, kv, _ := runStoreAgent(t, cfg, endTurnResp("done"))

	wm, ok, err := mem.LoadWorking(context.Background(),
		core.MemoryRef{AgentID: "support", ThreadKey: "thread-1"})
	if err != nil || !ok {
		t.Fatalf("working memory should have been stored (ok=%v err=%v)", ok, err)
	}
	if wm.Version != 1 {
		t.Errorf("a first write should be version 1, got %d", wm.Version)
	}
	env, err := decodeMemory(wm.Payload)
	if err != nil {
		t.Fatalf("stored payload should decode: %v", err)
	}
	if !hasAssistantText(env.Messages, "done") {
		t.Error("working memory should carry the assistant turn")
	}
	if _, found, _ := kv.Get(context.Background(), core.NamespaceUser, memoryKey("thread-1")); found {
		t.Error("an agent using the store should not also write the legacy KV transcript")
	}
}

// TestAgentStoreMigratesLegacyTranscript covers a conversation that was live
// before the store existed: it must carry on rather than restart, and the old
// key must not be left behind to diverge from the new one.
func TestAgentStoreMigratesLegacyTranscript(t *testing.T) {
	ctx, mem, kv := withFakeMemory(context.Background())
	prior := memoryEnvelope{Version: memoryVersion, Messages: []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: "earlier question"},
		{Role: core.LLMRoleAssistant, Text: "earlier answer"},
	}}
	encoded, err := json.Marshal(prior)
	if err != nil {
		t.Fatalf("encode prior: %v", err)
	}
	if _, err := kv.Set(ctx, core.NamespaceUser, memoryKey("thread-1"), encoded, 0); err != nil {
		t.Fatalf("seed legacy transcript: %v", err)
	}

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("new answer")}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn),
		storeAgentConfig("support"))
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	if len(conn.calls) == 0 || !hasAssistantText(conn.calls[0].Messages, "earlier answer") {
		t.Error("the run should have resumed from the legacy transcript")
	}
	if _, found, _ := kv.Get(ctx, core.NamespaceUser, memoryKey("thread-1")); found {
		t.Error("the legacy transcript should be removed once the thread is in the store")
	}
	if _, ok, _ := mem.LoadWorking(ctx, core.MemoryRef{AgentID: "support", ThreadKey: "thread-1"}); !ok {
		t.Error("the thread should now live in the store")
	}
}

// TestAgentStoreUnnamedAgentUsesLegacyPath is the compatibility guarantee: an
// agent that never named itself behaves exactly as it did before any of this,
// and stores nothing durable under a key a rename could destroy.
func TestAgentStoreUnnamedAgentUsesLegacyPath(t *testing.T) {
	mem, kv, _ := runStoreAgent(t, memoryAgentConfig(`"thread-1"`), endTurnResp("done"))

	if mem.threadCount() != 0 {
		t.Error("an agent with no agentId must not create a stored conversation")
	}
	if _, found, _ := kv.Get(context.Background(), core.NamespaceUser, memoryKey("thread-1")); !found {
		t.Error("an agent with no agentId should still write the legacy KV transcript")
	}
}

// TestAgentStoreHistoryOff checks that a block can keep working memory without
// recording anything a person could later read.
func TestAgentStoreHistoryOff(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.History = historyOff
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp("done"))

	if got := mem.turnsFor("support", "thread-1"); len(got) != 0 {
		t.Errorf("history: off should record no turns, got %d", len(got))
	}
	if _, ok, _ := mem.LoadWorking(context.Background(),
		core.MemoryRef{AgentID: "support", ThreadKey: "thread-1"}); !ok {
		t.Error("history: off should still checkpoint working memory")
	}
}

// TestAgentStoreVolatileImpliesHistoryOff covers the subagent case. A specialist
// in another agent's tool slot works a thread its caller minted for it, and
// recording those would accumulate a conversation row per delegation forever.
func TestAgentStoreVolatileImpliesHistoryOff(t *testing.T) {
	cfg := storeAgentConfig("specialist")
	cfg.MemoryVolatile = true
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp("done"))

	if got := mem.turnsFor("specialist", "thread-1"); len(got) != 0 {
		t.Errorf("volatile memory should not record durable history, got %d turns", len(got))
	}
}

// TestAgentStoreVolatileCanStillOptIntoHistory checks that the implication is a
// default and not a rule: an author who wants both can say so.
func TestAgentStoreVolatileCanStillOptIntoHistory(t *testing.T) {
	cfg := storeAgentConfig("specialist")
	cfg.MemoryVolatile = true
	cfg.History = historyRecord
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp("done"))

	if got := mem.turnsFor("specialist", "thread-1"); len(got) == 0 {
		t.Error("an explicit history: record should win over the volatile default")
	}
}

// TestAgentStoreRecordsUnansweredQuestion checks that a run which ends without
// an answer still leaves the question in the record. Somebody asked it.
func TestAgentStoreRecordsUnansweredQuestion(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Input = `"will this ever finish?"`
	cfg.MaxIterations = 1
	cfg.Default = &types.FlowConfig{Process: []types.BlockConfig{{Type: "record", Name: "guard"}}}

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{toolCallResp("noop", `{}`)}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	ctx, mem, _ := withFakeMemory(context.Background())
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := mem.turnsFor("support", "thread-1")
	if len(turns) != 1 {
		t.Fatalf("want the unanswered question recorded, got %d turns", len(turns))
	}
	var attrs turnAttrs
	if err := json.Unmarshal(turns[0].Attrs, &attrs); err != nil {
		t.Fatalf("decode attrs: %v", err)
	}
	if !attrs.Unanswered {
		t.Error("a question the run never answered should be marked unanswered")
	}
}

// TestAgentStoreSaveFailureDoesNotFailTheRun checks the priority: the person got
// their answer, and losing the record afterwards must not take that away.
func TestAgentStoreSaveFailureDoesNotFailTheRun(t *testing.T) {
	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("answered anyway")}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn),
		storeAgentConfig("support"))
	ctx, mem, _ := withFakeMemory(context.Background())
	mem.failWorking = true

	out, err := block.Process(ctx, aiMessage(t))
	if err != nil {
		t.Fatalf("a store that will not accept a write must not fail the run: %v", err)
	}
	if out == nil {
		t.Fatal("the run should still have produced its answer")
	}
}

// TestAgentStoreConfigRequiresAgentID names the two settings that cannot mean
// anything without somewhere to write, and checks the build says so rather than
// silently ignoring them.
func TestAgentStoreConfigRequiresAgentID(t *testing.T) {
	cases := []struct {
		name string
		want string
		cfg  func(*types.BlockConfig)
	}{
		{"history without agentId", "agentId", func(c *types.BlockConfig) { c.History = historyRecord }},
		{"userMemory without agentId", "agentId", func(c *types.BlockConfig) { c.UserMemory = true }},
		{"agentId without a thread", "memoryThreadId", func(c *types.BlockConfig) {
			c.AgentID = "support"
			c.MemoryThreadID = ""
		}},
		{"userMemory without a userId", "userId", func(c *types.BlockConfig) {
			c.AgentID = "support"
			c.UserMemory = true
		}},
		{"unknown history mode", "history", func(c *types.BlockConfig) {
			c.AgentID = "support"
			c.History = "sometimes"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := memoryAgentConfig(`"thread-1"`)
			tc.cfg(&cfg)
			err := validateAgentConfig(cfg)
			if err == nil {
				t.Fatal("want a build error naming the missing setting")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error should name %q, got %q", tc.want, err)
			}
		})
	}
}

// TestAgentUserMemoryRoundTrip drives the tools the model actually calls:
// remember something, then find it in the next run's request without having to
// ask for it.
func TestAgentUserMemoryRoundTrip(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.UserID = `"alice"`
	cfg.UserMemory = true

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp(rememberToolName, `{"name":"prefers-go","value":"Prefers Go examples."}`),
		endTurnResp("noted"),
	}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	ctx, mem, _ := withFakeMemory(context.Background())
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	stored, err := mem.Memories(ctx, core.MemoryRef{AgentID: "support", UserID: "alice"})
	if err != nil || len(stored) != 1 {
		t.Fatalf("want one stored memory, got %d (err=%v)", len(stored), err)
	}
	if stored[0].Name != "prefers-go" || stored[0].Value != "Prefers Go examples." {
		t.Errorf("stored the wrong memory: %+v", stored[0])
	}

	// A second run should be handed the memory without calling a tool for it.
	conn2 := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("ok")}}
	block2 := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn2), cfg)
	if _, err := block2.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("second process: %v", err)
	}
	if len(conn2.calls) == 0 {
		t.Fatal("the second run should have called the model")
	}
	if !requestMentions(conn2.calls[0].Messages, "Prefers Go examples.") {
		t.Error("what the agent remembered should reach the request without a recall tool call")
	}
}

// TestAgentUserMemoryStaysOutOfTheTranscript is the reason the preamble goes in
// the request rather than the conversation: stored with it, a memory corrected
// between runs would sit in working memory beside stale copies of itself.
func TestAgentUserMemoryStaysOutOfTheTranscript(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.UserID = `"alice"`
	cfg.UserMemory = true

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp(rememberToolName, `{"name":"prefers-go","value":"Prefers Go examples."}`),
		endTurnResp("noted"),
	}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	ctx, mem, _ := withFakeMemory(context.Background())
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	wm, ok, err := mem.LoadWorking(ctx, core.MemoryRef{AgentID: "support", ThreadKey: "thread-1"})
	if err != nil || !ok {
		t.Fatalf("working memory should exist (ok=%v err=%v)", ok, err)
	}
	if strings.Contains(string(wm.Payload), "previously chosen to remember") {
		t.Error("the memory preamble must not be persisted into working memory")
	}
}

// TestAgentUserMemoryForget checks that forgetting reaches the store, since a
// memory that survives being forgotten is worse than one never kept.
func TestAgentUserMemoryForget(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.UserID = `"alice"`
	cfg.UserMemory = true

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp(rememberToolName, `{"name":"prefers-go","value":"Prefers Go examples."}`),
		toolCallResp(forgetToolName, `{"name":"prefers-go"}`),
		endTurnResp("forgotten"),
	}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	ctx, mem, _ := withFakeMemory(context.Background())
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	stored, err := mem.Memories(ctx, core.MemoryRef{AgentID: "support", UserID: "alice"})
	if err != nil {
		t.Fatalf("read memories: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("the memory should be gone, got %d", len(stored))
	}
}

// TestAgentUserMemoryRememberReplaces checks that telling an agent something it
// already believes corrects the belief rather than failing.
func TestAgentUserMemoryRememberReplaces(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.UserID = `"alice"`
	cfg.UserMemory = true

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp(rememberToolName, `{"name":"lang","value":"Prefers Go."}`),
		toolCallResp(rememberToolName, `{"name":"lang","value":"Prefers Rust now."}`),
		endTurnResp("updated"),
	}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	ctx, mem, _ := withFakeMemory(context.Background())
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	stored, err := mem.Memories(ctx, core.MemoryRef{AgentID: "support", UserID: "alice"})
	if err != nil || len(stored) != 1 {
		t.Fatalf("want one memory after the correction, got %d (err=%v)", len(stored), err)
	}
	if stored[0].Value != "Prefers Rust now." {
		t.Errorf("the correction should have replaced the belief, got %q", stored[0].Value)
	}
}

// TestAgentMemoryToolNameCollision checks the build refuses to hand the model two
// tools with one name, one of which it could never reach.
func TestAgentMemoryToolNameCollision(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.UserID = `"alice"`
	cfg.UserMemory = true
	cfg.Tools = []types.ToolConfig{toolBranch(rememberToolName, "shadows the built-in", nil)}

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("x")}}
	_, err := (&builder{reg: agentRegistry(&seen), pool: pool.New(0, 0), deps: depsLLM(conn)}).block(cfg)
	if err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("want a reserved-name build error, got %v", err)
	}
}

// TestClearAgentMemoryReachesTheStore is the erasure guarantee: a clear that
// reached only the legacy key would report success while leaving a readable copy
// of the conversation behind.
func TestClearAgentMemoryReachesTheStore(t *testing.T) {
	ctx, mem, _ := withFakeMemory(context.Background())
	ref := core.MemoryRef{AgentID: "support", ThreadKey: "thread-1"}
	if _, err := mem.AppendTurns(ctx, ref, []core.Turn{
		{Role: core.LLMRoleUser, Text: "please forget this"},
	}); err != nil {
		t.Fatalf("seed turns: %v", err)
	}

	block, err := newClearAgentMemory(
		types.Settings{"threadId": `"thread-1"`, "agentId": "support"}, core.BlockDeps{})
	if err != nil {
		t.Fatalf("build clear-agent-memory: %v", err)
	}
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if mem.threadCount() != 0 {
		t.Error("clearing a thread must remove its stored conversation, not just the legacy key")
	}
}

// requestMentions reports whether any message in a request carries text.
func requestMentions(msgs []core.LLMMessage, text string) bool {
	for i := range msgs {
		if strings.Contains(msgs[i].Text, text) {
			return true
		}
	}
	return false
}

// TestAgentMemoryToolNamesAreNotClaimedWhenOff checks that an agent which never
// asked for user memory keeps its own tool of the same name. The builder only
// rejects that collision when userMemory is on, so a flow declaring a "remember"
// branch and no user memory builds — and must still reach its branch.
func TestAgentMemoryToolNamesAreNotClaimedWhenOff(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Tools = []types.ToolConfig{toolBranch(rememberToolName, "the flow's own tool", nil)}

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{
		toolCallResp(rememberToolName, `{"anything":true}`),
		endTurnResp("done"),
	}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	ctx, _, _ := withFakeMemory(context.Background())
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(seen) == 0 {
		t.Error("the flow's own remember tool should have run, not the built-in")
	}
}

// TestAgentMemoryPreambleIsBounded checks that what rides in front of every
// request is capped. Unbounded it is spent on every turn of every run, and an
// agent with a long history with somebody would reach the provider's window
// before reading a word of the conversation.
func TestAgentMemoryPreambleIsBounded(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.UserID = `"alice"`
	cfg.UserMemory = true

	ctx, mem, _ := withFakeMemory(context.Background())
	ref := core.MemoryRef{AgentID: "support", UserID: "alice"}
	for i := 0; i < preambleMaxMemories+20; i++ {
		if _, err := mem.PutMemory(ctx, ref, fmt.Sprintf("fact-%03d", i), "something worth keeping", 0); err != nil {
			t.Fatalf("seed memory %d: %v", i, err)
		}
	}

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("ok")}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	if len(conn.calls) == 0 {
		t.Fatal("want a model call")
	}
	preamble := conn.calls[0].Messages[0].Text
	if strings.Count(preamble, "\n- ") > preambleMaxMemories {
		t.Errorf("the preamble should render at most %d memories", preambleMaxMemories)
	}
	if !strings.Contains(preamble, searchMemoryToolName) {
		t.Error("the preamble should say the rest are reachable, not drop them silently")
	}
}

// TestTitleForTrimsOnARuneBoundary checks that a long non-ASCII opening turn is
// not cut mid-rune, which would store an invalid UTF-8 sequence.
func TestTitleForTrimsOnARuneBoundary(t *testing.T) {
	got := titleFor(strings.Repeat("é", titleMaxLen))
	if !utf8.ValidString(got) {
		t.Errorf("title should stay valid UTF-8, got %q", got)
	}
}

// TestAgentStoreDoesNotOverwriteWhatItCouldNotRead is the data-loss case.
//
// A run whose load failed has no idea what was stored, so its transcript is the
// current exchange alone. Writing that back would replace a conversation somebody
// has been having with the last thing they said — over a transient read failure.
func TestAgentStoreDoesNotOverwriteWhatItCouldNotRead(t *testing.T) {
	ctx, mem, _ := withFakeMemory(context.Background())
	ref := core.MemoryRef{AgentID: "support", ThreadKey: "thread-1"}

	// A conversation already in progress.
	prior := memoryEnvelope{Version: memoryVersion, Messages: []core.LLMMessage{
		{Role: core.LLMRoleUser, Text: "a long conversation"},
		{Role: core.LLMRoleAssistant, Text: "with real history in it"},
	}}
	encoded, err := json.Marshal(prior)
	if err != nil {
		t.Fatalf("encode prior: %v", err)
	}
	if _, err := mem.SaveWorking(ctx, ref, core.WorkingMemory{Payload: encoded}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	mem.failLoad = true
	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("answered blind")}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), storeAgentConfig("support"))
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("a store that cannot be read must not fail the run: %v", err)
	}

	mem.failLoad = false
	wm, ok, err := mem.LoadWorking(ctx, ref)
	if err != nil || !ok {
		t.Fatalf("the stored conversation should still be there: ok=%v err=%v", ok, err)
	}
	env, err := decodeMemory(wm.Payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !hasAssistantText(env.Messages, "with real history in it") {
		t.Error("a failed load let the run overwrite the conversation it could not see")
	}
}

// TestAgentMemoryPreambleClipsOneOversizedMemory checks the cap is enforced
// during the write and not only before it. Nothing bounds what an agent chooses
// to remember, so a single memory can be longer than the whole budget — and that
// is exactly the one worth capping.
func TestAgentMemoryPreambleClipsOneOversizedMemory(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.UserID = `"alice"`
	cfg.UserMemory = true

	ctx, mem, _ := withFakeMemory(context.Background())
	ref := core.MemoryRef{AgentID: "support", UserID: "alice"}
	if _, err := mem.PutMemory(ctx, ref, "essay", strings.Repeat("x", preambleMaxChars*3), 0); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var seen []any
	conn := &scriptedLLM{responses: []*core.LLMResponse{endTurnResp("ok")}}
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(conn), cfg)
	if _, err := block.Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}
	if len(conn.calls) == 0 {
		t.Fatal("want a model call")
	}
	// Generous slack for the framing around the memories; the point is that one
	// entry cannot carry the request past the budget by its own length.
	if got := len(conn.calls[0].Messages[0].Text); got > preambleMaxChars*2 {
		t.Errorf("one oversized memory should be clipped, preamble is %d bytes", got)
	}
}

// TestAgentStoreRejectsWhitespaceAgentID checks validation trims what the builder
// trims. An agentId of "   " that passed here would reach the builder as empty,
// and the block would quietly lose the history it had just been told to keep.
func TestAgentStoreRejectsWhitespaceAgentID(t *testing.T) {
	cfg := memoryAgentConfig(`"thread-1"`)
	cfg.AgentID = "   "
	cfg.History = historyRecord
	if err := validateAgentConfig(cfg); err == nil {
		t.Fatal("a whitespace-only agentId should be refused, not trimmed to nothing")
	}
}

// TestNewAgentBlockBuildsWithoutAnAgentID is the editor's path.
//
// The editor seeds a new block with every field that declares a schema default,
// so a default on `history` would write `history: record` into a block that has
// no agentId yet — a flow that does not build, produced by dropping a block on a
// canvas. The runtime's default lives in the builder instead, where it can see
// whether there is an agent to record under.
func TestNewAgentBlockBuildsWithoutAnAgentID(t *testing.T) {
	field, ok := reflect.TypeFor[aiAgentMeta]().FieldByName("History")
	if !ok {
		t.Fatal("aiAgentMeta has no History field")
	}
	if tag := field.Tag.Get("octo"); strings.Contains(tag, "default=") {
		t.Errorf("history must not carry a schema default — the editor seeds every field "+
			"that declares one, so a new ai-agent would be born with history set and no "+
			"agentId, which does not build. Tag: %s", tag)
	}

	// And the pairing that proves it: the block the editor would create builds, and
	// the runtime still defaults it to recording once an agent is named.
	if err := validateAgentConfig(memoryAgentConfig(`"thread-1"`)); err != nil {
		t.Errorf("a new ai-agent with no agentId should build: %v", err)
	}
}

// TestAgentStoreNamesAnUnansweredConversation pins what happens when a run ends
// without an answer — stopped, refused, out of turns.
//
// It still names it, and the chain is told the answer is empty. That is the
// honest arrangement: the question WAS asked and is recorded, so the conversation
// exists and a list has to show something for it. Whether an unanswered exchange
// is worth a name is a judgement, and judgements are the chain's — it can return
// nothing and get nothing, which is exactly what a "hi" that was cut off deserves.
func TestAgentStoreNamesAnUnansweredConversation(t *testing.T) {
	cfg := storeAgentConfig("support")
	cfg.Input = `"how do I get a refund?"`
	cfg.NameThread = &types.FlowConfig{Process: []types.BlockConfig{{
		Type:     "tool",
		Settings: types.Settings{"result": `"Refund, unanswered"`},
	}}}
	mem, _, _ := runStoreAgent(t, cfg, endTurnResp(""))

	turns := mem.turnsFor("support", "thread-1")
	if len(turns) == 0 {
		t.Fatal("the question should be recorded even unanswered")
	}
	if got := mem.titleOf("support", "thread-1"); got != "Refund, unanswered" {
		t.Errorf("an unanswered conversation is still named, got %q", got)
	}
}
