package anthropic

import (
	"encoding/json"
	"fmt"
	"log/slog"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/juancavallotti/octo/runtime/core"
)

// maxPauseContinuations caps how many times one turn is resumed after the
// provider paused its server-side tool loop, so a turn that keeps pausing ends
// instead of billing without bound.
const maxPauseContinuations = 5

// blockToolUse is the content block type of a tool call the caller runs.
const blockToolUse = "tool_use"

// portableBlocks are the content block types core.LLMMessage expresses without
// loss. A turn carrying any other type is kept verbatim as well.
var portableBlocks = map[string]bool{
	"text":              true,
	"thinking":          true,
	"redacted_thinking": true,
	blockToolUse:        true,
}

// pausedTurn assembles one assistant turn across the requests a paused server
// tool loop takes to finish.
//
// The provider pauses after a fixed number of server-side iterations and resumes
// when sent the turn so far as the trailing assistant message, answering with the
// continuation only. The turn is therefore the concatenation of every round's
// content, and what it cost is the sum of every round's usage.
type pausedTurn struct {
	base    []sdk.MessageParam
	content []sdk.ContentBlockUnion
	usage   sdk.Usage
	last    *sdk.Message
	rounds  int
}

// newPausedTurn starts assembling a turn for the conversation in params.
func newPausedTurn(params sdk.MessageNewParams) *pausedTurn {
	return &pausedTurn{base: params.Messages}
}

// add folds one round's response into the turn.
func (p *pausedTurn) add(message *sdk.Message) {
	p.content = append(p.content, message.Content...)
	p.usage.InputTokens += message.Usage.InputTokens
	p.usage.OutputTokens += message.Usage.OutputTokens
	p.usage.CacheReadInputTokens += message.Usage.CacheReadInputTokens
	p.usage.CacheCreationInputTokens += message.Usage.CacheCreationInputTokens
	p.usage.OutputTokensDetails.ThinkingTokens += message.Usage.OutputTokensDetails.ThinkingTokens
	p.last = message
}

// offset is the content index the next round's blocks start at, so a streamed
// event's index stays unique across the whole turn.
func (p *pausedTurn) offset() int { return len(p.content) }

// resume reports whether the turn paused and may be continued, and if so points
// params at the continuation request.
func (p *pausedTurn) resume(params *sdk.MessageNewParams) bool {
	if p.last == nil || p.last.StopReason != sdk.StopReasonPauseTurn {
		return false
	}
	if p.rounds >= maxPauseContinuations {
		slog.Warn("llm-anthropic turn still paused after the continuation cap; returning it as finished",
			"model", p.last.Model, "continuations", p.rounds)
		return false
	}
	p.rounds++
	soFar := make([]sdk.ContentBlockParamUnion, len(p.content))
	for i, block := range p.content {
		soFar[i] = block.ToParam()
	}
	messages := make([]sdk.MessageParam, 0, len(p.base)+1)
	messages = append(messages, p.base...)
	messages = append(messages, sdk.NewAssistantMessage(soFar...))
	params.Messages = messages
	return true
}

// message is the assembled turn: the last round's metadata, with every round's
// content and usage.
func (p *pausedTurn) message() *sdk.Message {
	out := *p.last
	out.Content = p.content
	out.Usage = p.usage
	return &out
}

// opaqueTurn returns the content verbatim, in request form, when it carries a
// block the portable fields cannot express, and nil otherwise.
func opaqueTurn(content []sdk.ContentBlockUnion) *core.LLMOpaqueTurn {
	needed := false
	for _, block := range content {
		if !portableBlocks[block.Type] {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}
	params := make([]sdk.ContentBlockParamUnion, 0, len(content))
	for _, block := range content {
		if block.AsAny() == nil {
			// A block type newer than this SDK has no request form to echo. Dropping
			// the opaque turn falls back to the portable fields, which lose the server
			// tool exchange but keep the conversation valid.
			slog.Warn("llm-anthropic cannot echo a content block of unknown type", "type", block.Type)
			return nil
		}
		params = append(params, block.ToParam())
	}
	raw, err := json.Marshal(params)
	if err != nil {
		slog.Warn("llm-anthropic cannot encode a server tool turn", "error", err)
		return nil
	}
	return &core.LLMOpaqueTurn{Provider: core.ProviderAnthropic, Content: raw}
}

// opaqueBlocks decodes an opaque turn this connector wrote back into the content
// it echoes.
func opaqueBlocks(raw json.RawMessage) ([]sdk.ContentBlockParamUnion, error) {
	var blocks []sdk.ContentBlockParamUnion
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return nil, fmt.Errorf("llm-anthropic: decode opaque assistant turn: %w", err)
	}
	return blocks, nil
}
