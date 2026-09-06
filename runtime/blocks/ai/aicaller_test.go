package ai

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// Every model call the runtime makes is recorded, whichever block made it. The
// ai-agent's own record shape is pinned by ai_trace_test.go and must not move;
// what these tests add is that the other AI blocks now report the same way.

// bareCaller wraps a client with no block identity, for a helper whose logic is
// under test rather than what it records.
func bareCaller(client core.LLMClient) *llmCaller {
	return &llmCaller{client: client}
}

// mapConfig is a minimal named ai-mapping block. It is built through the flow
// builder rather than newAIMapping directly, because the address a leaf reports
// only exists when the builder puts it there.
func mapConfig() types.BlockConfig {
	// connector is a leaf setting here, not the composite slot the ai-* composites
	// declare it as.
	return types.BlockConfig{
		Type:     blockTypeAIMapping,
		Name:     "reshape",
		Settings: types.Settings{"connector": "claude", "prompt": "reshape the body"},
	}
}

// mappingRegistry is the shared AI test registry. The ai-mapping leaf is already
// in it: the test registry inherits everything this package registers.
func mappingRegistry(seen *[]any) *core.BlockRegistry {
	return recordRegistry(seen)
}

// An ai-router calls the model once per inspection round, and each of those
// rounds is billed. A trace that recorded only the round that decided would
// under-report a router that looked at the message first.
func TestRouterTracesEveryRound(t *testing.T) {
	c := tracingAgent(t)

	var seen []any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(toolCallResp("get_body", `{}`), &core.LLMUsage{InputTokens: 40, OutputTokens: 8}),
		withUsage(toolCallResp("select_route", `{"route":"billing"}`),
			&core.LLMUsage{InputTokens: 90, OutputTokens: 12}),
	}}

	proc := mustBuildAI(t, recordRegistry(&seen), depsLLM(fake), routerConfig(true))
	if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 2 {
		t.Fatalf("llm.turn records = %d, want 2 (one per round)", len(turns))
	}
	for i, turn := range turns {
		if turn.BlockType != blockKindAIRouter {
			t.Errorf("round %d BlockType = %q, want %q", i, turn.BlockType, blockKindAIRouter)
		}
		if got := turn.Attrs[attrIteration]; got != i+1 {
			t.Errorf("round %d iteration = %v, want %d", i, got, i+1)
		}
		if got := turn.Attrs[attrConnector]; got != "claude" {
			t.Errorf("round %d connector = %v, want claude", i, got)
		}
		if _, ok := turn.Attrs[attrUsage]; !ok {
			t.Errorf("round %d reported no usage", i)
		}
	}
}

// An ai-retry calls the model once per repair attempt. Those are the calls a
// flapping flow spends its money on, so each one is recorded.
func TestRetryTracesEveryAttempt(t *testing.T) {
	c := tracingAgent(t)

	var seen []any
	// The first revision does not satisfy require-field, so the loop asks again.
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		reviseResp(`{"body":{"nope":1}}`),
		reviseResp(`{"body":{"amount":42}}`),
	}}

	proc := mustBuildAI(t, retryRegistry(&seen), depsLLM(fake), retryConfig(3, false))
	if _, err := proc.Process(context.Background(), newMessageBody(t, `{}`)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 2 {
		t.Fatalf("llm.turn records = %d, want 2 (one per attempt)", len(turns))
	}
	for i, turn := range turns {
		if turn.BlockType != blockKindAIRetry {
			t.Errorf("attempt %d BlockType = %q, want %q", i, turn.BlockType, blockKindAIRetry)
		}
		if got := turn.Attrs[attrIteration]; got != i+1 {
			t.Errorf("attempt %d iteration = %v, want %d", i, got, i+1)
		}
	}
}

// An ai-mapping calls the model once per message, so its record carries no
// iteration — there is no loop to number. It is a leaf block, which means the
// address on its record can only have come from BlockDeps.
func TestMappingTracesItsOneCall(t *testing.T) {
	c := tracingAgent(t)

	var seen []any
	fake := &scriptedLLM{repeat: withUsage(endTurnResp(`{"ok":true}`),
		&core.LLMUsage{InputTokens: 12, OutputTokens: 4})}

	proc := mustBuildAI(t, mappingRegistry(&seen), depsLLM(fake), mapConfig())
	if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 1 {
		t.Fatalf("llm.turn records = %d, want 1", len(turns))
	}
	if turns[0].BlockType != blockTypeAIMapping {
		t.Errorf("BlockType = %q, want %q", turns[0].BlockType, blockTypeAIMapping)
	}
	if got := turns[0].Attrs[attrBlock]; got != "reshape" {
		t.Errorf("block = %v, want reshape — a leaf learns its name only from deps", got)
	}
	if got := turns[0].Path; got != "test.reshape" {
		t.Errorf("path = %q, want the address the builder minted", got)
	}
	if _, ok := turns[0].Attrs[attrIteration]; ok {
		t.Error("a block that calls the model once numbered its one call")
	}
}

// The projection every block's usage goes through is the one the ai-agent's
// events path already reports, so the two observers of a call cannot disagree
// about what it cost.
func TestTraceCarriesUsageForEveryBlockKind(t *testing.T) {
	usage := &core.LLMUsage{InputTokens: 150, OutputTokens: 25, ThinkingTokens: 10, CachedTokens: 90}

	tests := []struct {
		name string
		// response is the reply that lets each block finish in one model call, so
		// the record under test is the only one.
		response func() *core.LLMResponse
		run      func(t *testing.T, resp *core.LLMResponse)
	}{
		{
			name:     "ai-router",
			response: func() *core.LLMResponse { return toolCallResp("select_route", `{"route":"billing"}`) },
			run: func(t *testing.T, resp *core.LLMResponse) {
				var seen []any
				fake := &scriptedLLM{responses: []*core.LLMResponse{resp}}
				proc := mustBuildAI(t, recordRegistry(&seen), depsLLM(fake), routerConfig(true))
				if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
					t.Fatalf("process: %v", err)
				}
			},
		},
		{
			name:     "ai-retry",
			response: func() *core.LLMResponse { return reviseResp(`{"body":{"amount":42}}`) },
			run: func(t *testing.T, resp *core.LLMResponse) {
				var seen []any
				fake := &scriptedLLM{responses: []*core.LLMResponse{resp}}
				proc := mustBuildAI(t, retryRegistry(&seen), depsLLM(fake), retryConfig(1, false))
				if _, err := proc.Process(context.Background(), newMessageBody(t, `{}`)); err != nil {
					t.Fatalf("process: %v", err)
				}
			},
		},
		{
			name:     "ai-mapping",
			response: func() *core.LLMResponse { return endTurnResp(`{"ok":true}`) },
			run: func(t *testing.T, resp *core.LLMResponse) {
				var seen []any
				fake := &scriptedLLM{repeat: resp}
				proc := mustBuildAI(t, mappingRegistry(&seen), depsLLM(fake), mapConfig())
				if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
					t.Fatalf("process: %v", err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := tracingAgent(t)
			resp := withUsage(tc.response(), usage)
			resp.Model = "claude-sonnet-4-6-20260101"
			tc.run(t, resp)

			turns := c.turns()
			if len(turns) == 0 {
				t.Fatal("no llm.turn record")
			}
			if got := turns[0].Attrs[attrModel]; got != "claude-sonnet-4-6-20260101" {
				t.Errorf("model = %v, want the snapshot that served the call", got)
			}
			got, ok := turns[0].Attrs[attrUsage].(map[string]any)
			if !ok {
				t.Fatalf("usage = %T, want a map", turns[0].Attrs[attrUsage])
			}
			for field, want := range usageFields(usage) {
				if got[field] != want {
					t.Errorf("usage[%s] = %v, want %v", field, got[field], want)
				}
			}
		})
	}
}

// A call that failed is the one worth reading — a refusal, a timeout, a provider
// outage. Recording only the successes would make a trace of a failing flow look
// empty, whichever block was failing.
func TestFailedCallsAreRecordedForEveryBlockKind(t *testing.T) {
	tests := []struct {
		name      string
		blockType string
		run       func(t *testing.T, llm core.Connector)
	}{
		{
			name:      "ai-router",
			blockType: blockKindAIRouter,
			run: func(t *testing.T, llm core.Connector) {
				var seen []any
				proc := mustBuildAI(t, recordRegistry(&seen), depsLLM(llm), routerConfig(true))
				if _, err := proc.Process(context.Background(), aiMessage(t)); err == nil {
					t.Fatal("a failing model produced no error")
				}
			},
		},
		{
			name:      "ai-mapping",
			blockType: blockTypeAIMapping,
			run: func(t *testing.T, llm core.Connector) {
				var seen []any
				proc := mustBuildAI(t, mappingRegistry(&seen), depsLLM(llm), mapConfig())
				if _, err := proc.Process(context.Background(), aiMessage(t)); err == nil {
					t.Fatal("a failing model produced no error")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := tracingAgent(t)
			tc.run(t, &failingLLM{err: errors.New("provider unavailable")})

			turns := c.turns()
			if len(turns) != 1 {
				t.Fatalf("llm.turn records = %d, want 1", len(turns))
			}
			if turns[0].BlockType != tc.blockType {
				t.Errorf("BlockType = %q, want %q", turns[0].BlockType, tc.blockType)
			}
			if turns[0].Err == "" {
				t.Error("the failed call recorded no error")
			}
			if _, ok := turns[0].Attrs[attrUsage]; ok {
				t.Error("a failed call reported usage")
			}
		})
	}
}

// Summarizing an agent's memory is a real, billed model call its own turn loop
// never sees. It is billed to the agent — that is where the money is spent and
// where `memoryCompaction: summarize` would be turned off — but marked with its
// purpose and carrying no iteration, so it cannot be read as a turn the agent took.
func TestMemoryCompactionIsBilledToTheAgentButNotItsTurns(t *testing.T) {
	c := tracingAgent(t)
	ctx, _ := withFakeServices(context.Background())

	cfg := memoryAgentConfig(`"t1"`)
	cfg.Name = "chat"
	cfg.Settings["memoryCompaction"] = memoryCompactSummarize
	// Small enough that one answer overruns it, so compaction runs on the first pass.
	cfg.Settings["contextMaxTokens"] = 10

	var seen []any
	fake := &scriptedLLM{responses: []*core.LLMResponse{
		withUsage(endTurnResp(strings.Repeat("a", 400)), &core.LLMUsage{InputTokens: 20, OutputTokens: 100}),
		withUsage(endTurnResp("SUMMARY"), &core.LLMUsage{InputTokens: 120, OutputTokens: 6}),
	}}

	if _, err := mustBuildAI(t, agentRegistry(&seen), depsLLM(fake), cfg).Process(ctx, aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 2 {
		t.Fatalf("llm.turn records = %d, want 2 (the agent's turn and the compaction)", len(turns))
	}

	turn, compaction := turns[0], turns[1]
	if got := turn.Attrs[attrIteration]; got != 1 {
		t.Errorf("the agent's turn iteration = %v, want 1", got)
	}
	if _, ok := turn.Attrs[attrPurpose]; ok {
		t.Error("an ordinary agent turn carried a purpose")
	}

	if compaction.BlockType != blockKindAIAgent || compaction.Path != turn.Path {
		t.Errorf("compaction landed at %q/%q, want the agent's own %q/%q",
			compaction.BlockType, compaction.Path, turn.BlockType, turn.Path)
	}
	if got := compaction.Attrs[attrPurpose]; got != purposeMemory {
		t.Errorf("compaction purpose = %v, want %q", got, purposeMemory)
	}
	if _, ok := compaction.Attrs[attrIteration]; ok {
		t.Error("the compaction call numbered itself as one of the agent's iterations")
	}
	if _, ok := compaction.Attrs[attrUsage]; !ok {
		t.Error("the compaction call reported no usage — the turn it hides is still billed")
	}
}

// A streamed turn is billed exactly like a blocking one, so it has to be recorded
// exactly like one: one record per turn rather than one per delta, with the same
// usage and the same model. Switching an agent to stream changes when a caller
// hears about a turn, never what the turn cost.
func TestAStreamedTurnRecordsTheSameAsABlockingOne(t *testing.T) {
	usage := &core.LLMUsage{InputTokens: 150, OutputTokens: 25, ThinkingTokens: 10, CachedTokens: 90}

	// One run of the agent, either streaming or not, over the same two turns.
	run := func(t *testing.T, streaming bool) []types.TraceEvent {
		t.Helper()
		c := tracingAgent(t)

		turns := []*core.LLMResponse{
			withUsage(toolCallResp("look", `{"q":"refund"}`), usage),
			withUsage(endTurnResp(`{"answer":"refunded"}`), usage),
		}
		for _, turn := range turns {
			turn.Model = "claude-sonnet-4-6-20260101"
		}

		var llm core.Connector = &scriptedLLM{responses: turns}
		if streaming {
			llm = &streamingLLM{
				scriptedLLM: scriptedLLM{responses: turns},
				deltas: [][]core.LLMStreamEvent{{
					{Kind: core.LLMStreamText, Text: "one"},
					{Kind: core.LLMStreamText, Text: "two"},
					{Kind: core.LLMStreamText, Text: "three"},
				}},
			}
		}

		var seen []any
		var events []map[string]any
		path := eventsPath(nil)
		cfg := agentWithEvents(&path, nil, streaming)
		proc := mustBuildAI(t, eventsRegistry(&seen, &events), depsLLM(llm), cfg)
		if _, err := proc.Process(context.Background(), aiMessage(t)); err != nil {
			t.Fatalf("process (streaming=%v): %v", streaming, err)
		}
		return c.turns()
	}

	blocking := run(t, false)
	streamed := run(t, true)

	if len(blocking) != 2 {
		t.Fatalf("blocking llm.turn records = %d, want 2", len(blocking))
	}
	if len(streamed) != len(blocking) {
		t.Fatalf("streamed llm.turn records = %d, want %d — a stream must not record per delta",
			len(streamed), len(blocking))
	}
	for i := range blocking {
		for _, attr := range []string{attrIteration, attrModel, attrStopReason, attrToolCalls} {
			if streamed[i].Attrs[attr] != blocking[i].Attrs[attr] {
				t.Errorf("turn %d %s: streamed %v, blocking %v",
					i, attr, streamed[i].Attrs[attr], blocking[i].Attrs[attr])
			}
		}
		want, _ := blocking[i].Attrs[attrUsage].(map[string]any)
		got, _ := streamed[i].Attrs[attrUsage].(map[string]any)
		if len(want) == 0 {
			t.Fatalf("turn %d blocking record carried no usage", i)
		}
		for field, value := range want {
			if got[field] != value {
				t.Errorf("turn %d usage[%s]: streamed %v, blocking %v", i, field, got[field], value)
			}
		}
	}
}

// embeds reads the llm.embed records a collector saw.
func (c *turnCollector) embeds() []types.TraceEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []types.TraceEvent
	for _, record := range c.records {
		if record.Kind == types.TraceLLMEmbed {
			out = append(out, record)
		}
	}
	return out
}

// embedConfig is a named ai-embed block over a two-element batch.
func embedConfig() types.BlockConfig {
	return types.BlockConfig{
		Type: blockTypeAIEmbed,
		Name: "vectorize",
		Settings: types.Settings{
			"connector": "claude",
			"model":     "text-embedding-3-small",
			"text":      `[body.text, body.text]`,
		},
	}
}

// embedRegistry is the shared AI test registry plus the ai-embed leaf.
func embedRegistry(seen *[]any) *core.BlockRegistry {
	reg := recordRegistry(seen)
	return reg
}

// An embedding call is billed like any other, so it gets a record — with the size
// of the batch it sent, since one call over many texts is what an ai-embed costs.
func TestEmbedTracesTheCall(t *testing.T) {
	c := tracingAgent(t)

	fake := &fakeEmbedder{resp: &core.EmbedResponse{
		Vectors: [][]float32{{0.1}, {0.2}},
		Usage:   &core.EmbedUsage{InputTokens: 7},
		Model:   "text-embedding-3-small",
	}}

	var seen []any
	proc := mustBuildAI(t, embedRegistry(&seen), depsLLM(fake), embedConfig())
	if _, err := proc.Process(context.Background(), newMessageBody(t, `{"text":"hello"}`)); err != nil {
		t.Fatalf("process: %v", err)
	}

	embeds := c.embeds()
	if len(embeds) != 1 {
		t.Fatalf("llm.embed records = %d, want 1", len(embeds))
	}
	record := embeds[0]
	if record.BlockType != blockTypeAIEmbed {
		t.Errorf("BlockType = %q, want %q", record.BlockType, blockTypeAIEmbed)
	}
	if got := record.Attrs[attrBlock]; got != "vectorize" {
		t.Errorf("block = %v, want vectorize", got)
	}
	if got := record.Attrs[attrBatch]; got != 2 {
		t.Errorf("batch = %v, want 2", got)
	}
	if got := record.Attrs[attrModel]; got != "text-embedding-3-small" {
		t.Errorf("model = %v, want the one that served the call", got)
	}
	usage, ok := record.Attrs[attrUsage].(map[string]any)
	if !ok || usage["inputTokens"] != 7 {
		t.Errorf("usage = %v, want inputTokens 7", record.Attrs[attrUsage])
	}
	// An embedding has none of these, and reporting them as zeros would claim the
	// provider said something it never did.
	for _, absent := range []string{attrStopReason, attrToolCalls, attrIteration} {
		if _, present := record.Attrs[absent]; present {
			t.Errorf("llm.embed carried %q, which an embedding has no such thing as", absent)
		}
	}
}

// Gemini's embeddings API reports no token count at all. The record still has to
// exist — the call happened and was billed — and must say nothing about tokens
// rather than say zero, which would read as a call that cost nothing.
func TestEmbedRecordsAProviderThatReportsNoTokens(t *testing.T) {
	c := tracingAgent(t)

	fake := &fakeEmbedder{resp: &core.EmbedResponse{
		Vectors: [][]float32{{0.1}, {0.2}},
		Model:   "gemini-embedding-001",
	}}

	var seen []any
	proc := mustBuildAI(t, embedRegistry(&seen), depsLLM(fake), embedConfig())
	if _, err := proc.Process(context.Background(), newMessageBody(t, `{"text":"hello"}`)); err != nil {
		t.Fatalf("process: %v", err)
	}

	embeds := c.embeds()
	if len(embeds) != 1 {
		t.Fatalf("llm.embed records = %d, want 1", len(embeds))
	}
	if _, ok := embeds[0].Attrs[attrUsage]; ok {
		t.Error("a provider that reported no tokens was recorded as having reported some")
	}
	if got := embeds[0].Attrs[attrModel]; got != "gemini-embedding-001" {
		t.Errorf("model = %v, want it named even with no usage to price", got)
	}
}

// A failed embedding still names the model: the request says what was going to be
// billed even when nothing came back to confirm it.
func TestEmbedRecordsAFailedCall(t *testing.T) {
	c := tracingAgent(t)

	fake := &fakeEmbedder{err: errors.New("provider unavailable")}
	var seen []any
	proc := mustBuildAI(t, embedRegistry(&seen), depsLLM(fake), embedConfig())
	if _, err := proc.Process(context.Background(), newMessageBody(t, `{"text":"hello"}`)); err == nil {
		t.Fatal("a failing embedder produced no error")
	}

	embeds := c.embeds()
	if len(embeds) != 1 {
		t.Fatalf("llm.embed records = %d, want 1", len(embeds))
	}
	if embeds[0].Err == "" {
		t.Error("the failed call recorded no error")
	}
	if got := embeds[0].Attrs[attrModel]; got != "text-embedding-3-small" {
		t.Errorf("model = %v, want the requested id", got)
	}
	if _, ok := embeds[0].Attrs[attrUsage]; ok {
		t.Error("a failed call reported usage")
	}
}

// With tracing off nothing is built at all. This runs once per model call, on
// every AI block in the process, so the guard has to cover all of them and not
// just the one it was written for.
func TestNothingIsPublishedWhenTracingIsOff(t *testing.T) {
	c := tracingAgent(t)
	core.SetTracer(nil)

	var seen []any
	router := mustBuildAI(t, recordRegistry(&seen), depsLLM(&scriptedLLM{
		repeat: toolCallResp("select_route", `{"route":"billing"}`),
	}), routerConfig(true))
	if _, err := router.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("router: %v", err)
	}

	retry := mustBuildAI(t, retryRegistry(&seen), depsLLM(&scriptedLLM{
		repeat: reviseResp(`{"body":{"amount":42}}`),
	}), retryConfig(1, false))
	if _, err := retry.Process(context.Background(), newMessageBody(t, `{}`)); err != nil {
		t.Fatalf("retry: %v", err)
	}

	mapper := mustBuildAI(t, mappingRegistry(&seen), depsLLM(&scriptedLLM{
		repeat: endTurnResp(`{"ok":true}`),
	}), mapConfig())
	if _, err := mapper.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("mapping: %v", err)
	}

	embedder := mustBuildAI(t, embedRegistry(&seen), depsLLM(&fakeEmbedder{
		resp: &core.EmbedResponse{Vectors: [][]float32{{0.1}, {0.2}}},
	}), embedConfig())
	if _, err := embedder.Process(context.Background(), newMessageBody(t, `{"text":"hello"}`)); err != nil {
		t.Fatalf("embed: %v", err)
	}

	if turns := c.turns(); len(turns) != 0 {
		t.Fatalf("llm.turn records = %d with tracing off, want 0", len(turns))
	}
	if embeds := c.embeds(); len(embeds) != 0 {
		t.Fatalf("llm.embed records = %d with tracing off, want 0", len(embeds))
	}
}

// providedLLM and providedEmbedder are the fakes plus a vendor family. Both
// embed the concrete fake rather than core.Connector: embedding the interface
// would promote only its methods, and the resolver's assertion for LLMClient or
// EmbedClient would then fail.
type providedLLM struct {
	*scriptedLLM
	family string
}

func (p providedLLM) Provider() string { return p.family }

type providedEmbedder struct {
	*fakeEmbedder
	family string
}

func (p providedEmbedder) Provider() string { return p.family }

// TestTraceCarriesTheProviderThatServedTheCall covers #254 on the turn path.
//
// The connector's name is the author's ("claude" here, but it could be
// "my-llm"), and the model id is not a substitute: gpt-4o is published under
// both OPENAI and AZURE. The consumer keys its cached-token arithmetic on the
// provider, so a guess is a wrong cost rather than a cosmetic gap.
func TestTraceCarriesTheProviderThatServedTheCall(t *testing.T) {
	c := tracingAgent(t)

	llm := providedLLM{
		scriptedLLM: &scriptedLLM{repeat: endTurnResp("done")},
		family:      core.ProviderAnthropic,
	}
	var seen []any
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(llm), agentTurnConfig())
	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 1 {
		t.Fatalf("llm.turn records = %d, want 1", len(turns))
	}
	if got := turns[0].Attrs[attrProvider]; got != core.ProviderAnthropic {
		t.Errorf("provider = %v, want %s", got, core.ProviderAnthropic)
	}
	// Still the authored connector name, not the vendor: they answer different
	// questions and a cost reader needs both.
	if got := turns[0].Attrs[attrConnector]; got != "claude" {
		t.Errorf("connector = %v, want the authored name claude", got)
	}
}

// The same attribute on llm.embed, since embeddings are priced from their own
// rate card and need the family for the same reason.
func TestEmbedTraceCarriesTheProvider(t *testing.T) {
	c := tracingAgent(t)

	fake := providedEmbedder{
		fakeEmbedder: &fakeEmbedder{resp: &core.EmbedResponse{
			Vectors: [][]float32{{0.1}},
			Model:   "text-embedding-3-small",
		}},
		family: core.ProviderOpenAI,
	}
	var seen []any
	proc := mustBuildAI(t, embedRegistry(&seen), depsLLM(fake), embedConfig())
	if _, err := proc.Process(context.Background(), newMessageBody(t, `{"text":"hello"}`)); err != nil {
		t.Fatalf("process: %v", err)
	}

	embeds := c.embeds()
	if len(embeds) != 1 {
		t.Fatalf("llm.embed records = %d, want 1", len(embeds))
	}
	if got := embeds[0].Attrs[attrProvider]; got != core.ProviderOpenAI {
		t.Errorf("provider = %v, want %s", got, core.ProviderOpenAI)
	}
}

// A third-party connector predating core.LLMProvider keeps working, and its
// records carry no provider rather than a guessed one — the consumer then falls
// back to inferring from the model id, exactly as it did for every record
// written before the attribute existed.
func TestTraceOmitsTheProviderWhenAConnectorReportsNone(t *testing.T) {
	c := tracingAgent(t)

	var seen []any
	llm := &scriptedLLM{repeat: endTurnResp("done")} // no Provider method
	block := mustBuildAI(t, agentRegistry(&seen), depsLLM(llm), agentTurnConfig())
	if _, err := block.Process(context.Background(), aiMessage(t)); err != nil {
		t.Fatalf("process: %v", err)
	}

	turns := c.turns()
	if len(turns) != 1 {
		t.Fatalf("llm.turn records = %d, want 1", len(turns))
	}
	if _, present := turns[0].Attrs[attrProvider]; present {
		t.Errorf("provider = %v, want it absent rather than empty or guessed",
			turns[0].Attrs[attrProvider])
	}
}
