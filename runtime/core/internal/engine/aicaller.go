// This file provides the seam every AI block calls its model through, and the
// trace record that seam writes for each call.
//
// The record is bound to the call rather than to the caller on purpose. A model
// call is the only thing in the runtime that costs money per invocation, and an
// LLM block that reports what it spent only because its author remembered to is a
// block that will eventually stop reporting. Binding it here — to the value
// resolveLLM hands out, which is the only way to reach a provider from the engine
// — makes the record a property of calling a model rather than a courtesy.
package engine

import (
	"context"
	"time"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

// The attributes a model-call record carries. They are named here rather than
// spelled at each SetAttr so the vocabulary a cost reader parses is enumerable
// from one place.
const (
	attrBlock      = "block"
	attrConnector  = "connector"
	attrIteration  = "iteration"
	attrStopReason = "stopReason"
	attrToolCalls  = "toolCalls"
	attrModel      = "model"
	attrUsage      = "usage"
	attrPurpose    = "purpose"
	attrBatch      = "batch"
)

// purposeMemory marks the model call an ai-agent makes to compact its own memory:
// billed to the agent's block, but not one of the turns its loop took.
const purposeMemory = "memory-compaction"

// callerIdentity is who is calling the model — fixed when the block is built, so
// a record never has to work it out per call.
type callerIdentity struct {
	blockType string
	connector string
	flow      string
	path      string
	name      string
}

// newIdentity reads a block's identity off its build-time deps. The address is the
// one the flow builder minted, so a model-call record lands at the same place a
// block event for the same block would.
func newIdentity(blockType, connector string, deps core.BlockDeps) callerIdentity {
	return callerIdentity{
		blockType: blockType,
		connector: connector,
		flow:      deps.Address.Flow,
		path:      deps.Address.Path,
		name:      deps.Address.Name,
	}
}

// record starts the record for one provider call, filling in everything known
// before the provider answers; the caller adds what came back.
//
// It reads the live message — TraceID reads Variables, a plain map — which is safe
// only because every call site is synchronous on the flow's own goroutine, inside
// the block's Process. Publishing a model call from anywhere else would need the
// snapshot-first treatment the block listener uses.
func (id callerIdentity) record(
	kind types.TraceKind, msg *types.Message, started time.Time,
) types.TraceEvent {
	event := types.TraceEvent{
		Kind:          kind,
		TraceID:       msg.TraceID(),
		EventID:       msg.EventID,
		CorrelationID: msg.CorrelationID,
		Flow:          id.flow,
		Path:          id.path,
		BlockType:     id.blockType,
		OccurredAt:    time.Now(),
		DurationNs:    time.Since(started).Nanoseconds(),
		Attrs:         map[string]any{attrConnector: id.connector},
	}
	if id.name != "" {
		event.SetAttr(attrBlock, id.name)
	}
	return event
}

// callTrace is the decision to record one provider call, taken before the call is
// made. Taking it once is both cheaper and more honest than checking afterwards:
// with tracing off it reads one atomic and never touches the clock, and a
// publisher installed while a call was already in flight cannot produce a record
// whose duration is measured from the zero time.
type callTrace struct {
	tracer  core.TracePublisher
	started time.Time
}

func beginTrace() callTrace {
	tracer := core.Tracer()
	if !tracer.Enabled() {
		return callTrace{}
	}
	return callTrace{tracer: tracer, started: time.Now()}
}

// on reports whether this call is being recorded.
func (t callTrace) on() bool { return t.tracer != nil }

// turnLabel is what distinguishes one model call of a block from another: which
// pass of its own loop this was, and — for a call that is not one of the block's
// ordinary turns — what it was for.
//
// It is a value rather than a map because it is on the path of every model call,
// and a map that usually holds one integer is an allocation to say nothing.
type turnLabel struct {
	// iteration is one-based, matching what the ai-agent's events path already
	// reports. It is zero for a block that calls the model once per message and so
	// has no loop to number. An ai-router's round and an ai-retry's attempt are the
	// same fact under different local names — the n-th model call this block made
	// for this message — so they share the one attribute rather than teaching a
	// cost reader three spellings of it.
	iteration int
	// purpose marks a call that is not one of the block's own turns. Empty for one
	// that is, so an ordinary turn carries no attribute at all.
	purpose string
}

// apply writes the label onto a record, omitting what does not apply.
func (l turnLabel) apply(event *types.TraceEvent) {
	if l.iteration > 0 {
		event.SetAttr(attrIteration, l.iteration)
	}
	if l.purpose != "" {
		event.SetAttr(attrPurpose, l.purpose)
	}
}

// llmCaller is how every AI block in the engine calls its model. It is what
// resolveLLM hands out, so there is no way for a block to reach a provider and
// skip the record.
//
// It is written once at build time and read-only afterwards: one instance is
// shared across every worker of a flow, exactly as the core.LLMClient it wraps is.
// Nothing here may become mutable state.
type llmCaller struct {
	client core.LLMClient
	// streamer is the same connector as client, asserted to its streaming half. It
	// is nil for a provider that has none.
	streamer core.LLMStreamClient
	who      callerIdentity
}

// streams reports whether the provider behind this caller can stream.
func (c *llmCaller) streams() bool { return c.streamer != nil }

// complete runs one blocking model turn and records what it cost.
func (c *llmCaller) complete(
	ctx context.Context, msg *types.Message, req core.LLMRequest, label turnLabel,
) (*core.LLMResponse, error) {
	trace := beginTrace()
	resp, err := c.client.Complete(ctx, req)
	c.traceTurn(trace, msg, resp, label, err)
	return resp, err
}

// stream runs one streamed model turn and records what it cost.
//
// One record per turn, not one per delta: a streamed turn returns the same
// response a blocking one does, so the two paths converge here and a trace reads
// identically whether or not the block streams.
func (c *llmCaller) stream(
	ctx context.Context, msg *types.Message, req core.LLMRequest,
	on func(core.LLMStreamEvent) error, label turnLabel,
) (*core.LLMResponse, error) {
	trace := beginTrace()
	resp, err := c.streamer.Stream(ctx, req, on)
	c.traceTurn(trace, msg, resp, label, err)
	return resp, err
}

// traceTurn records one model turn: what was billed for it, which model actually
// served it, and how long it took.
//
// A failed turn is recorded too, with the error and no usage. Those are the turns
// worth reading: a refusal, a timeout, a provider outage. Recording only the
// successes would make a trace of a failing block look empty.
func (c *llmCaller) traceTurn(
	trace callTrace, msg *types.Message, resp *core.LLMResponse, label turnLabel, callErr error,
) {
	if !trace.on() {
		return
	}
	record := c.who.record(types.TraceLLMTurn, msg, trace.started)
	label.apply(&record)
	if callErr != nil {
		record.Err = callErr.Error()
	}
	if resp != nil {
		record.SetAttr(attrStopReason, string(resp.StopReason))
		record.SetAttr(attrToolCalls, len(resp.ToolCalls))
		if resp.Model != "" {
			record.SetAttr(attrModel, resp.Model)
		}
		if resp.Usage != nil {
			// The same projection turnEndFields uses, so the trace and the
			// user-facing events path cannot disagree about what a turn cost.
			record.SetAttr(attrUsage, usageFields(resp.Usage))
		}
	}
	trace.tracer.Publish(record)
}

// embedCaller is llmCaller's counterpart for the embeddings capability, and what
// resolveEmbed hands out. It is a separate type rather than a method on llmCaller
// because the two share no call shape: an embedding takes a batch and returns
// vectors, has no conversation, and is priced from its own rate card.
//
// Like llmCaller it is written once at build time and read-only afterwards.
type embedCaller struct {
	client core.EmbedClient
	who    callerIdentity
}

// embed runs one embedding call and records what it cost.
func (c *embedCaller) embed(
	ctx context.Context, msg *types.Message, req core.EmbedRequest,
) (*core.EmbedResponse, error) {
	trace := beginTrace()
	resp, err := c.client.Embed(ctx, req)
	c.traceEmbed(trace, msg, req, resp, err)
	return resp, err
}

// traceEmbed records one embedding call: how many texts it sent, which model
// served them, and what the provider charged.
//
// A failed call is recorded on the same terms a failed turn is, and still names
// the model — the request says what was going to be billed even when nothing came
// back to confirm it.
func (c *embedCaller) traceEmbed(
	trace callTrace, msg *types.Message, req core.EmbedRequest,
	resp *core.EmbedResponse, callErr error,
) {
	if !trace.on() {
		return
	}
	record := c.who.record(types.TraceLLMEmbed, msg, trace.started)
	record.SetAttr(attrBatch, len(req.Input))
	record.SetAttr(attrModel, servedEmbedModel(resp, req.Model))
	if callErr != nil {
		record.Err = callErr.Error()
	}
	// A nil usage is left off rather than written as zeros: the provider reporting
	// nothing and the provider charging nothing are different facts, and Gemini's
	// embeddings API always reports nothing.
	if resp != nil && resp.Usage != nil {
		record.SetAttr(attrUsage, embedUsageFields(resp.Usage))
	}
	trace.tracer.Publish(record)
}

// embedUsageFields projects an embedding call's token accounting. It is the
// counterpart of usageFields, and deliberately not the same shape: an embedding
// has no output, no reasoning and no cache, so reporting those as zeros would
// claim the provider said something it did not.
func embedUsageFields(usage *core.EmbedUsage) map[string]any {
	return map[string]any{"inputTokens": usage.InputTokens}
}

// servedEmbedModel is the model that answered, falling back to the one that was
// asked for — including on a failure, where there is no response but the request
// still names the model that was going to be billed.
func servedEmbedModel(resp *core.EmbedResponse, requested string) string {
	if resp != nil && resp.Model != "" {
		return resp.Model
	}
	return requested
}
