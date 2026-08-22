package openrouter

import (
	"encoding/json"
	"fmt"

	sdk "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/shared"

	"github.com/juancavallotti/octo/runtime/core"
)

// The wire names of the fields OpenRouter adds to the OpenAI schema. They are
// named here rather than spelled at each use so the set of extensions this
// connector depends on can be read in one place.
const (
	fieldReasoning        = "reasoning"
	fieldReasoningDetails = "reasoning_details"
	fieldCost             = "cost"
	// A count of tokens, not a credential, whatever the name looks like.
	fieldCacheWriteTokens = "cache_write_tokens" //nolint:gosec // G101 false positive
)

// The finish reasons Chat Completions names. Unlike Responses, a turn says
// outright why it stopped, so this connector maps a reason rather than inferring
// one from the shape of the output.
const (
	finishToolCalls     = "tool_calls"
	finishLength        = "length"
	finishContentFilter = "content_filter"
)

// turn is one assistant turn, gathered from either path before it becomes a
// core.LLMResponse.
//
// It exists because Chat Completions has no terminal object carrying the finished
// response, so a streamed turn has to be folded together from deltas. Both paths
// build one of these and translateTurn is the only thing that turns one into a
// response, which is what makes a streamed turn and a blocking turn the same
// answer by construction rather than by two translations agreeing.
type turn struct {
	text      string
	reasoning string
	// reasoningDetails is echoed back verbatim on the next turn. It is opaque on
	// purpose: it carries provider-specific signatures a thinking model validates,
	// and anything this connector understood about it would be a thing it could
	// get wrong.
	reasoningDetails json.RawMessage
	toolCalls        []core.LLMToolCall
	finishReason     string
	refused          bool
	model            string
	usage            *core.LLMUsage
}

// translateTurn folds a gathered turn into the agnostic response.
func translateTurn(t turn, configuredModel string) *core.LLMResponse {
	var thinking []core.LLMThinkingBlock
	if t.reasoning != "" || len(t.reasoningDetails) > 0 {
		// Reasoning, not the answer. Keeping it out of Text is what stops a caller
		// that folds the answer into a message body from publishing it.
		thinking = []core.LLMThinkingBlock{{
			Text:     t.reasoning,
			Redacted: t.reasoningDetails,
		}}
	}

	out := &core.LLMResponse{
		Text:       t.text,
		ToolCalls:  t.toolCalls,
		StopReason: mapStopReason(t.finishReason, len(t.toolCalls) > 0, t.refused),
		Usage:      t.usage,
		Model:      servedBy(t.model, configuredModel),
	}
	out.Raw = core.LLMMessage{
		Role:      core.LLMRoleAssistant,
		Text:      out.Text,
		Thinking:  thinking,
		ToolCalls: t.toolCalls,
	}
	return out
}

// turnFromCompletion gathers a blocking response into a turn.
func turnFromCompletion(resp *sdk.ChatCompletion) (turn, error) {
	if resp == nil || len(resp.Choices) == 0 {
		return turn{}, fmt.Errorf("llm-openrouter: the response carried no choices")
	}
	choice := resp.Choices[0]
	msg := choice.Message

	t := turn{
		text:         msg.Content,
		finishReason: choice.FinishReason,
		refused:      msg.Refusal != "",
		model:        resp.Model,
		usage:        translateUsage(resp.Usage),
	}

	var reasoning string
	if decodeExtra(msg.JSON.ExtraFields[fieldReasoning].Raw(), &reasoning) {
		t.reasoning = reasoning
	}
	if raw := msg.JSON.ExtraFields[fieldReasoningDetails].Raw(); raw != "" && raw != jsonNull {
		t.reasoningDetails = json.RawMessage(raw)
	}

	for _, tc := range msg.ToolCalls {
		fn := tc.Function
		var input json.RawMessage
		if fn.Arguments != "" {
			input = json.RawMessage(fn.Arguments)
		}
		t.toolCalls = append(t.toolCalls, core.LLMToolCall{ID: tc.ID, Name: fn.Name, Input: input})
	}
	return t, nil
}

// translateUsage converts the reported accounting, reporting nil when the
// response carried none.
//
// CompletionTokens already counts reasoning tokens inside itself, matching the
// inclusive convention core.LLMUsage adopts, and PromptTokens already counts the
// tokens served from cache — the OpenAI-compatible convention, which is why this
// connector reports OPENROUTER rather than the vendor behind the model.
func translateUsage(u sdk.CompletionUsage) *core.LLMUsage {
	if u.PromptTokens == 0 && u.CompletionTokens == 0 {
		return nil
	}
	usage := &core.LLMUsage{
		InputTokens:    int(u.PromptTokens),
		OutputTokens:   int(u.CompletionTokens),
		ThinkingTokens: int(u.CompletionTokensDetails.ReasoningTokens),
		CachedTokens:   int(u.PromptTokensDetails.CachedTokens),
	}

	// Only some upstreams charge for cache creation, and only those report it.
	var cacheWrites int
	if decodeExtra(u.PromptTokensDetails.JSON.ExtraFields[fieldCacheWriteTokens].Raw(), &cacheWrites) {
		usage.CacheWriteTokens = cacheWrites
	}

	// The reason this connector asks for usage accounting at all: what the call
	// actually cost, rather than what a rate card would estimate it at.
	var cost float64
	if decodeExtra(u.JSON.ExtraFields[fieldCost].Raw(), &cost) {
		usage.ReportedCostUSD = &cost
	}
	return usage
}

// servedBy is the model that answered, preferring what the provider echoed over
// what the connector asked for. The two differ whenever a configured alias
// resolves to a dated snapshot, and it is the snapshot that was billed.
func servedBy(reported, configured string) string {
	if reported != "" {
		return reported
	}
	return configured
}

// mapStopReason maps the turn's finish reason to the agnostic stop reason.
//
// The presence of tool calls outranks the reason, because a turn that asked for
// tools is a tool turn whatever else it also did — and OpenRouter's upstreams do
// not all spell that reason the same way.
func mapStopReason(reason string, hasCalls, refused bool) core.LLMStopReason {
	if refused {
		return core.LLMStopRefusal
	}
	if hasCalls || reason == finishToolCalls {
		return core.LLMStopToolUse
	}
	switch reason {
	case finishLength:
		return core.LLMStopMaxTokens
	case finishContentFilter:
		return core.LLMStopRefusal
	default:
		return core.LLMStopEndTurn
	}
}

// toMessages converts the conversation to Chat Completions messages, with the
// system prompt as the leading system message rather than a field of its own.
func toMessages(system string, msgs []core.LLMMessage) ([]sdk.ChatCompletionMessageParamUnion, error) {
	out := make([]sdk.ChatCompletionMessageParamUnion, 0, len(msgs)+1)
	if system != "" {
		out = append(out, sdk.SystemMessage(system))
	}
	for i, m := range msgs {
		switch m.Role {
		case core.LLMRoleUser:
			out = append(out, sdk.UserMessage(m.Text))
		case core.LLMRoleAssistant:
			out = append(out, assistantMessage(m))
		case core.LLMRoleTool:
			for _, tr := range m.ToolResults {
				content := tr.Content
				if tr.IsError {
					content = "ERROR: " + tr.Content
				}
				out = append(out, sdk.ToolMessage(content, tr.ToolCallID))
			}
		default:
			return nil, fmt.Errorf("llm-openrouter: unknown message role %q at index %d", m.Role, i)
		}
	}
	return out, nil
}

// assistantMessage builds one assistant turn: its text, its tool calls, and the
// reasoning it is echoing back.
//
// The reasoning goes back as the opaque block it arrived as. A thinking model
// asked to continue across a tool call validates the signatures inside it, so a
// block this connector rebuilt from the readable summary would be a new block the
// upstream has never seen — which is worse than sending nothing.
func assistantMessage(m core.LLMMessage) sdk.ChatCompletionMessageParamUnion {
	msg := sdk.ChatCompletionAssistantMessageParam{}
	if m.Text != "" {
		msg.Content = sdk.ChatCompletionAssistantMessageParamContentUnion{
			OfString: param.NewOpt(m.Text),
		}
	}
	for _, tc := range m.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, sdk.ChatCompletionMessageToolCallUnionParam{
			OfFunction: &sdk.ChatCompletionMessageFunctionToolCallParam{
				ID: tc.ID,
				Function: sdk.ChatCompletionMessageFunctionToolCallFunctionParam{
					Name:      tc.Name,
					Arguments: string(tc.Input),
				},
			},
		})
	}
	for _, tb := range m.Thinking {
		if len(tb.Redacted) == 0 {
			continue
		}
		msg.SetExtraFields(map[string]any{fieldReasoningDetails: json.RawMessage(tb.Redacted)})
		break
	}
	return sdk.ChatCompletionMessageParamUnion{OfAssistant: &msg}
}

// toTools converts tool definitions to SDK function tools, decoding each JSON
// Schema into the SDK's parameters map.
//
// Strict is off, and has to be: it requires every property listed in required and
// additionalProperties false throughout. A flow author writes an inputSchema by
// hand and this connector passes it through verbatim, so enforcing a shape the
// author never agreed to would reject ordinary tools — starting with the empty
// `{"type":"object"}` an agent tool defaults to.
func toTools(tools []core.LLMTool) ([]sdk.ChatCompletionToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]sdk.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		params := map[string]any{}
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("llm-openrouter: tool %q input schema: %w", t.Name, err)
			}
		}
		fn := shared.FunctionDefinitionParam{
			Name:       t.Name,
			Parameters: params,
			Strict:     param.NewOpt(false),
		}
		if t.Description != "" {
			fn.Description = param.NewOpt(t.Description)
		}
		out = append(out, sdk.ChatCompletionToolUnionParam{
			OfFunction: &sdk.ChatCompletionFunctionToolParam{Function: fn},
		})
	}
	return out, nil
}

// toToolChoice maps the agnostic tool choice to the SDK union. The second return
// is false for the auto (zero) mode, signalling the caller to leave it unset.
func toToolChoice(tc core.LLMToolChoice) (sdk.ChatCompletionToolChoiceOptionUnionParam, bool) {
	switch tc.Mode {
	case core.LLMToolChoiceAny:
		return sdk.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("required"),
		}, true
	case core.LLMToolChoiceNone:
		return sdk.ChatCompletionToolChoiceOptionUnionParam{
			OfAuto: param.NewOpt("none"),
		}, true
	case core.LLMToolChoiceTool:
		return sdk.ChatCompletionToolChoiceOptionUnionParam{
			OfFunctionToolChoice: &sdk.ChatCompletionNamedToolChoiceParam{
				Function: sdk.ChatCompletionNamedToolChoiceFunctionParam{Name: tc.Name},
			},
		}, true
	default:
		return sdk.ChatCompletionToolChoiceOptionUnionParam{}, false
	}
}

// translateEmbedResponse reorders the SDK's index-tagged embeddings back into
// request order and converts each to float32, carrying the model that served the
// call and what it charged for it.
func translateEmbedResponse(
	resp *sdk.CreateEmbeddingResponse, want int, requestedModel string,
) (*core.EmbedResponse, error) {
	if resp == nil || len(resp.Data) != want {
		return nil, fmt.Errorf("llm-openrouter embed: response had %d embeddings, want %d",
			len(embeddings(resp)), want)
	}
	vectors := make([][]float32, want)
	for _, d := range resp.Data {
		if d.Index < 0 || int(d.Index) >= want {
			return nil, fmt.Errorf("llm-openrouter embed: response embedding index %d out of range", d.Index)
		}
		vectors[d.Index] = toFloat32(d.Embedding)
	}
	return &core.EmbedResponse{
		Vectors: vectors,
		Usage:   translateEmbedUsage(resp.Usage),
		Model:   servedBy(resp.Model, requestedModel),
	}, nil
}

// embeddings is resp.Data with a nil response tolerated, so the error above can
// report a count without dereferencing what it is complaining about.
func embeddings(resp *sdk.CreateEmbeddingResponse) []sdk.Embedding {
	if resp == nil {
		return nil
	}
	return resp.Data
}

// translateEmbedUsage converts the reported token count, reporting nil when the
// response carried none. Only the prompt total is taken: an embedding produces no
// output, so the SDK's TotalTokens is the same number under another name.
func translateEmbedUsage(u sdk.CreateEmbeddingResponseUsage) *core.EmbedUsage {
	if u.PromptTokens == 0 {
		return nil
	}
	return &core.EmbedUsage{InputTokens: int(u.PromptTokens)}
}

// toFloat32 narrows the SDK's float64 embedding values to the runtime's float32
// vector representation.
func toFloat32(in []float64) []float32 {
	out := make([]float32, len(in))
	for i, v := range in {
		out[i] = float32(v)
	}
	return out
}
