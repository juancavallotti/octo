package openrouter

import (
	"encoding/json"
	"fmt"
	"sort"

	sdk "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/packages/ssestream"

	"github.com/juancavallotti/octo/runtime/core"
)

// toolCallFold accumulates one tool call across the chunks that build it. A call
// is announced once with its id and name and then extended with argument
// fragments, so those two are latched while the arguments are appended.
type toolCallFold struct {
	index int
	id    string
	name  string
	args  []byte
}

// streamFold gathers a streamed turn.
//
// Text and reasoning are concatenated because they arrive as fragments; the
// finish reason, the model and the usage are latched because each arrives whole
// and later chunks either repeat it or omit it. Summing the usage — the idiom a
// naive accumulator reaches for — would multiply every count by the number of
// chunks carrying one.
type streamFold struct {
	text      []byte
	reasoning []byte
	// details is latched rather than concatenated: OpenRouter sends the block
	// whole, and the last one it sends is the complete one.
	details      json.RawMessage
	calls        map[int]*toolCallFold
	finishReason string
	refused      bool
	model        string
	usage        *core.LLMUsage
}

// foldStream reads the stream to its end, reporting deltas as they pass and
// returning the turn they add up to.
func foldStream(
	stream *ssestream.Stream[sdk.ChatCompletionChunk], on func(core.LLMStreamEvent) error,
) (turn, error) {
	fold := streamFold{calls: map[int]*toolCallFold{}}
	seen := false

	for stream.Next() {
		chunk := stream.Current()
		seen = true
		if err := fold.absorb(chunk, on); err != nil {
			return turn{}, err
		}
	}
	if err := stream.Err(); err != nil {
		return turn{}, fmt.Errorf("llm-openrouter stream: %w", err)
	}
	if !seen {
		return turn{}, fmt.Errorf("llm-openrouter stream: ended without any chunks")
	}
	return fold.turn(), nil
}

// absorb folds one chunk in and emits whatever of it is a delta.
func (f *streamFold) absorb(chunk sdk.ChatCompletionChunk, on func(core.LLMStreamEvent) error) error {
	if chunk.Model != "" {
		f.model = chunk.Model
	}
	// The usage-only chunk that stream_options asks for carries no choices, which
	// is why usage is read before the choice is.
	if usage := translateUsage(chunk.Usage); usage != nil {
		f.usage = usage
	}
	if len(chunk.Choices) == 0 {
		return nil
	}

	choice := chunk.Choices[0]
	if choice.FinishReason != "" {
		f.finishReason = choice.FinishReason
	}
	return f.absorbDelta(choice.Delta, on)
}

// absorbDelta folds one choice's delta in and emits it.
func (f *streamFold) absorbDelta(
	delta sdk.ChatCompletionChunkChoiceDelta, on func(core.LLMStreamEvent) error,
) error {
	if delta.Content != "" {
		f.text = append(f.text, delta.Content...)
		if err := on(core.LLMStreamEvent{Kind: core.LLMStreamText, Text: delta.Content}); err != nil {
			return err
		}
	}

	var reasoning string
	if decodeExtra(delta.JSON.ExtraFields[fieldReasoning].Raw(), &reasoning) && reasoning != "" {
		f.reasoning = append(f.reasoning, reasoning...)
		if err := on(core.LLMStreamEvent{Kind: core.LLMStreamThinking, Text: reasoning}); err != nil {
			return err
		}
	}
	if raw := delta.JSON.ExtraFields[fieldReasoningDetails].Raw(); raw != "" && raw != jsonNull {
		f.details = json.RawMessage(raw)
	}

	// A refusal is content, but it is not the answer, so it is not text. There is
	// no canonical kind for it and inventing one would grow the vocabulary for a
	// single case, which is what custom exists to avoid.
	if delta.Refusal != "" {
		f.refused = true
		if err := on(core.LLMStreamEvent{
			Kind: core.LLMStreamCustom, Name: "refusal", Text: delta.Refusal,
		}); err != nil {
			return err
		}
	}

	return f.absorbToolCalls(delta.ToolCalls, on)
}

// absorbToolCalls folds argument fragments into the call they belong to.
func (f *streamFold) absorbToolCalls(
	calls []sdk.ChatCompletionChunkChoiceDeltaToolCall, on func(core.LLMStreamEvent) error,
) error {
	for _, tc := range calls {
		index := int(tc.Index)
		call, known := f.calls[index]
		if !known {
			call = &toolCallFold{index: index}
			f.calls[index] = call
		}
		if tc.ID != "" {
			call.id = tc.ID
		}
		if tc.Function.Name != "" {
			call.name = tc.Function.Name
		}
		if tc.Function.Arguments == "" {
			continue
		}
		call.args = append(call.args, tc.Function.Arguments...)
		// Labelled with the call it belongs to, which a fragment does not name:
		// the id and the name are announced once, on the chunk that opens the call.
		if err := on(core.LLMStreamEvent{
			Kind:       core.LLMStreamToolInput,
			Text:       tc.Function.Arguments,
			Tool:       call.name,
			ToolCallID: call.id,
			Index:      index,
		}); err != nil {
			return err
		}
	}
	return nil
}

// turn renders what was folded.
func (f *streamFold) turn() turn {
	return turn{
		text:             string(f.text),
		reasoning:        string(f.reasoning),
		reasoningDetails: f.details,
		toolCalls:        f.toolCalls(),
		finishReason:     f.finishReason,
		refused:          f.refused,
		model:            f.model,
		usage:            f.usage,
	}
}

// toolCalls renders the folded calls in the order the provider indexed them. Map
// iteration order is not that order, and a turn whose calls came out shuffled
// would not be the turn Complete returned.
//
// A turn that asked for no tools yields nil rather than an empty slice, for the
// same reason: that is what the blocking path produces, and the two are compared
// for equality.
func (f *streamFold) toolCalls() []core.LLMToolCall {
	if len(f.calls) == 0 {
		return nil
	}

	indexes := make([]int, 0, len(f.calls))
	for index := range f.calls {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	calls := make([]core.LLMToolCall, 0, len(indexes))
	for _, index := range indexes {
		call := f.calls[index]
		var input json.RawMessage
		if len(call.args) > 0 {
			input = json.RawMessage(call.args)
		}
		calls = append(calls, core.LLMToolCall{ID: call.id, Name: call.name, Input: input})
	}
	return calls
}
