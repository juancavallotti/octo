// Package openai provides the "llm-openai" connector: a configured OpenAI
// Responses client that satisfies core.LLMClient so the AI flow elements can
// drive it interchangeably with the other providers. It translates the
// provider-agnostic core.LLM* DTOs to and from the OpenAI SDK types on each
// Complete call.
//
// It speaks Responses rather than Chat Completions, and the reason is reasoning.
// On /v1/chat/completions a non-none reasoning effort could not be combined with
// function tools on some models — the server answered 400 naming reasoning_effort
// — so an agent had to ask for no reasoning at all to be able to call its tools.
// That is not a small loss: a model told not to reason stops following the parts
// of a prompt that need any, and one told to answer in prose started answering
// with a copy of the JSON body it was handed. Responses has no such conflict.
//
// Chat Completions also returned no reasoning content whatsoever, so
// core.LLMMessage.Thinking was always empty here and thinking never streamed.
// Responses returns reasoning summaries, so both now work.
//
// Requests are stateless: store is off and the whole conversation is sent every
// turn, matching how the other two connectors work and keeping nothing on the
// provider's side. That is why reasoning items are echoed with their encrypted
// content — with no stored response to refer back to, the encrypted item is what
// lets the next turn continue the same reasoning.
package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strings"

	sdk "github.com/openai/openai-go/v2"
	"github.com/openai/openai-go/v2/option"
	"github.com/openai/openai-go/v2/packages/param"
	"github.com/openai/openai-go/v2/responses"
	"github.com/openai/openai-go/v2/shared"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterConnector("llm-openai", func() core.Connector {
		return &Connector{}
	})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "llm-openai",
		Label:    "OpenAI",
		Icon:     "Sparkles",
		Category: "llm",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

const defaultModel = "gpt-5.4"

// connectorSettings is the configuration decoded from the connector's settings.
type connectorSettings struct {
	// Authenticates with the OpenAI API; source from ${OPENAI_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Model id.
	Model string `json:"model" octo:"label=Model,default=gpt-5.4"`
	// Default response token cap (0 = the model default); a request may override it.
	MaxTokens int `json:"maxTokens" octo:"label=Max tokens"`
	// How much reasoning effort a reasoning-capable model spends. "default" sends no
	// reasoning field at all and lets the model choose, which is the only setting
	// that is also safe on a model with no reasoning to configure.
	//nolint:lll // a struct tag cannot be wrapped, and the enum has to list every option
	Reasoning string `json:"reasoning" octo:"label=Reasoning,type=enum,enum=default|none|minimal|low|medium|high,default=default"`
	// Overrides the API endpoint (for proxies, Azure, or OpenAI-compatible servers).
	BaseURL string `json:"baseURL" octo:"label=Base URL"`
}

const (
	// reasoningNone asks the model for no reasoning at all, on the wire.
	reasoningNone = "none"
	// reasoningDefault omits reasoning_effort so the model applies its own.
	reasoningDefault = "default"
)

// Connector is a configured OpenAI client that AI elements call through. It is
// safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    sdk.Client
	model     string
	maxTokens int
	reasoning shared.ReasoningEffort
}

var (
	_ core.Connector       = (*Connector)(nil)
	_ core.LLMClient       = (*Connector)(nil)
	_ core.LLMStreamClient = (*Connector)(nil)
	_ core.EmbedClient     = (*Connector)(nil)
)

// Start parses the settings, validates the API key, and builds the client so a
// bad configuration fails at startup rather than on first request.
func (c *Connector) Start(_ context.Context, config types.ConnectorConfig) error {
	var set connectorSettings
	if err := config.Settings.Decode(&set); err != nil {
		return err
	}
	if strings.TrimSpace(set.APIKey) == "" {
		return fmt.Errorf("llm-openai connector %q: apiKey is required", config.Name)
	}

	c.model = set.Model
	if c.model == "" {
		c.model = defaultModel
	}
	c.maxTokens = set.MaxTokens

	reasoning, err := toReasoningEffort(set.Reasoning)
	if err != nil {
		return fmt.Errorf("llm-openai connector %q: %w", config.Name, err)
	}
	c.reasoning = reasoning

	opts := []option.RequestOption{option.WithAPIKey(set.APIKey)}
	if set.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(set.BaseURL))
	}
	c.client = sdk.NewClient(opts...)

	slog.Info("llm-openai connector started",
		"connector", config.Name,
		"model", c.model,
		"maxTokens", c.maxTokens,
		"reasoning", set.Reasoning,
	)
	return nil
}

// toReasoningEffort validates the setting at startup, returning the empty effort
// for the default so the request carries no reasoning field at all.
//
// Unset means default, and default means silent. The reasoning field is documented
// for gpt-5 and o-series models only, and this connector's baseURL points it at
// Azure, proxies and other OpenAI-compatible servers whose model list is not
// OpenAI's — so sending the field unasked is how a connector pointed at a model
// without reasoning fails on every call. Saying nothing works everywhere and lets
// a reasoning model apply the effort it was trained to.
//
// The previous default was an explicit none, which existed only to dodge the
// Chat Completions conflict between reasoning and function tools. Responses has no
// such conflict, so the workaround is gone with the API that needed it — and with
// it the behaviour it caused, of a model with reasoning switched off answering in
// the shape of its input rather than the shape its prompt asked for.
func toReasoningEffort(effort string) (shared.ReasoningEffort, error) {
	switch effort {
	case "", reasoningDefault:
		return "", nil
	case reasoningNone, string(shared.ReasoningEffortMinimal), string(shared.ReasoningEffortLow),
		string(shared.ReasoningEffortMedium), string(shared.ReasoningEffortHigh):
		return shared.ReasoningEffort(effort), nil
	default:
		return "", fmt.Errorf(
			"reasoning must be one of default, none, minimal, low, medium, high, got %q", effort)
	}
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Provider names the vendor family, in the vocabulary the price catalogue uses.
// It is fixed rather than derived from baseURL: this connector speaks the OpenAI
// API and its token accounting whether it is pointed at OpenAI, Azure, or any
// other compatible server, and that accounting is what the figure downstream
// depends on.
func (c *Connector) Provider() string { return core.ProviderOpenAI }

// Complete runs one Responses turn, translating the request to SDK params and the
// response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-openai complete: %w", err)
	}
	return translateResponse(resp, c.model)
}

// Stream runs one Responses turn over the streaming endpoint, reporting content as
// it arrives and returning the same response Complete would have.
//
// The finished response arrives whole on the terminal response.completed event, so
// there is no accumulator here and no fold: the deltas are reported as they pass
// and the server's own final object is what gets translated. That is the closest
// of the three connectors to the equality the interface promises, since the
// streamed and blocking paths translate literally the same type.
func (c *Connector) Stream(
	ctx context.Context, req core.LLMRequest, on func(core.LLMStreamEvent) error,
) (*core.LLMResponse, error) {
	params, err := c.params(req)
	if err != nil {
		return nil, err
	}

	stream := c.client.Responses.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	// Function-call deltas name neither the tool nor the call; both are announced
	// once, on the output_item.added that opens the call. They are kept by output
	// index so an argument fragment can be labelled with the call it belongs to.
	opened := map[int64]responses.ResponseOutputItemUnion{}
	var final *responses.Response

	for stream.Next() {
		event := stream.Current()
		switch event.Type {
		case "response.output_item.added":
			opened[event.OutputIndex] = event.Item
		case "response.completed", "response.incomplete":
			resp := event.Response
			final = &resp
		case "response.failed":
			if msg := event.Response.Error.Message; msg != "" {
				return nil, fmt.Errorf("llm-openai stream: %s", msg)
			}
			return nil, fmt.Errorf("llm-openai stream: the response failed")
		}
		if emitErr := emitEvent(event, opened, on); emitErr != nil {
			return nil, emitErr
		}
	}
	if streamErr := stream.Err(); streamErr != nil {
		return nil, fmt.Errorf("llm-openai stream: %w", streamErr)
	}
	if final == nil {
		return nil, fmt.Errorf("llm-openai stream: ended without a completed response")
	}
	return translateResponse(final, c.model)
}

// emitEvent maps one stream event onto the canonical vocabulary.
//
// Only the deltas map. The lifecycle events carry nothing a caller cannot read off
// the returned response, which is the same rule the other two connectors follow.
func emitEvent(
	event responses.ResponseStreamEventUnion,
	opened map[int64]responses.ResponseOutputItemUnion,
	on func(core.LLMStreamEvent) error,
) error {
	index := int(event.OutputIndex)
	switch event.Type {
	case "response.output_text.delta":
		return on(core.LLMStreamEvent{Kind: core.LLMStreamText, Text: event.Delta, Index: index})

	// The summary is the only reasoning Responses returns as readable text; the
	// reasoning itself comes back encrypted. Streaming the summary is what fills the
	// gap between a question and the first word of an answer, which on a reasoning
	// model is most of the wait.
	case "response.reasoning_summary_text.delta":
		return on(core.LLMStreamEvent{Kind: core.LLMStreamThinking, Text: event.Delta, Index: index})

	case "response.function_call_arguments.delta":
		ev := core.LLMStreamEvent{Kind: core.LLMStreamToolInput, Text: event.Delta, Index: index}
		if item, ok := opened[event.OutputIndex]; ok {
			ev.Tool, ev.ToolCallID = item.Name, item.CallID
		}
		return on(ev)

	// A refusal is content, but it is not the answer, so it is not text. There is no
	// canonical kind for it and inventing one would grow the vocabulary for a single
	// provider, which is what custom exists to avoid.
	case "response.refusal.delta":
		return on(core.LLMStreamEvent{
			Kind: core.LLMStreamCustom, Name: "refusal", Text: event.Delta, Index: index,
		})

	default:
		return nil
	}
}

// params builds the SDK request shared by Complete and Stream, so the two paths
// cannot drift in what they ask the model for.
func (c *Connector) params(req core.LLMRequest) (responses.ResponseNewParams, error) {
	input, err := toInput(req.Messages)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	tools, err := toTools(req.Tools)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}

	params := responses.ResponseNewParams{
		Model: c.model,
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		// Nothing is kept on the provider's side: the conversation is sent whole every
		// turn, exactly as the other two connectors send theirs.
		Store: param.NewOpt(false),
	}
	if strings.TrimSpace(req.System) != "" {
		params.Instructions = param.NewOpt(req.System)
	}
	maxTokens := c.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}
	if maxTokens > 0 {
		params.MaxOutputTokens = param.NewOpt(int64(maxTokens))
	}
	if c.reasoning != "" {
		params.Reasoning = shared.ReasoningParam{Effort: c.reasoning}
		// A summary is the only way reasoning becomes visible, and asking for one is
		// free when there is reasoning to summarize. It is not asked for alongside an
		// explicit none, where there would be nothing to summarize.
		if c.reasoning != reasoningNone {
			params.Reasoning.Summary = shared.ReasoningSummaryAuto
		}
		// Reasoning that is not stored has to travel back on the next turn, and it can
		// only do that encrypted — without this the echoed items carry no content and
		// the server rejects the turn that follows a tool call.
		params.Include = []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		}
	}
	if len(tools) > 0 {
		params.Tools = tools
		if choice, ok := toToolChoice(req.ToolChoice); ok {
			params.ToolChoice = choice
		}
	}
	return params, nil
}

// Embed embeds a batch of texts in one request, translating the SDK's
// index-tagged results back into request order — the SDK documents input as an
// array but doesn't promise the response preserves that order.
func (c *Connector) Embed(ctx context.Context, req core.EmbedRequest) (*core.EmbedResponse, error) {
	if len(req.Input) == 0 {
		return nil, fmt.Errorf("llm-openai embed: input is required")
	}

	params := sdk.EmbeddingNewParams{
		Input: sdk.EmbeddingNewParamsInputUnion{OfArrayOfStrings: req.Input},
		Model: req.Model,
	}
	if req.Dimensions > 0 {
		params.Dimensions = param.NewOpt(int64(req.Dimensions))
	}

	resp, err := c.client.Embeddings.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-openai embed: %w", err)
	}
	return translateEmbedResponse(resp, len(req.Input), req.Model)
}

// translateEmbedResponse reorders the SDK's index-tagged embeddings back into
// request order and converts each to float32, carrying the model that served the
// call and what it charged for it.
func translateEmbedResponse(
	resp *sdk.CreateEmbeddingResponse, want int, requestedModel string,
) (*core.EmbedResponse, error) {
	if len(resp.Data) != want {
		return nil, fmt.Errorf("llm-openai embed: response had %d embeddings, want %d", len(resp.Data), want)
	}
	vectors := make([][]float32, want)
	for _, d := range resp.Data {
		if d.Index < 0 || int(d.Index) >= want {
			return nil, fmt.Errorf("llm-openai embed: response embedding index %d out of range", d.Index)
		}
		vectors[d.Index] = toFloat32(d.Embedding)
	}
	return &core.EmbedResponse{
		Vectors: vectors,
		Usage:   translateEmbedUsage(resp.Usage),
		Model:   servedBy(resp.Model, requestedModel),
	}, nil
}

// translateEmbedUsage converts the SDK's token count, reporting nil when the
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

// toInput converts the conversation to Responses input items. An assistant turn
// becomes several items — reasoning, then text, then one per tool call — because
// Responses models a turn as a flat list rather than as one message with parts.
//
// The order is load-bearing. A reasoning item has to precede the function call it
// produced, or the server rejects the turn as a call whose reasoning is missing;
// it is the same rule Anthropic enforces on echoed thinking blocks, and it is why
// both connectors put reasoning first.
func toInput(msgs []core.LLMMessage) (responses.ResponseInputParam, error) {
	out := make(responses.ResponseInputParam, 0, len(msgs))
	for i, m := range msgs {
		switch m.Role {
		case core.LLMRoleUser:
			out = append(out, responses.ResponseInputItemParamOfMessage(m.Text, responses.EasyInputMessageRoleUser))
		case core.LLMRoleAssistant:
			out = append(out, assistantItems(m)...)
		case core.LLMRoleTool:
			for _, tr := range m.ToolResults {
				content := tr.Content
				if tr.IsError {
					content = "ERROR: " + tr.Content
				}
				out = append(out, responses.ResponseInputItemParamOfFunctionCallOutput(tr.ToolCallID, content))
			}
		default:
			return nil, fmt.Errorf("llm-openai: unknown message role %q at index %d", m.Role, i)
		}
	}
	return out, nil
}

// assistantItems builds one assistant turn's items: reasoning, then text, then the
// function calls.
//
// A reasoning block with no signature is dropped rather than sent. The signature is
// the item id the server matches the echo against, and an item invented without one
// is not the model's reasoning being returned — it is a new item the server has
// never seen, which is worse than saying nothing.
func assistantItems(m core.LLMMessage) []responses.ResponseInputItemUnionParam {
	items := make([]responses.ResponseInputItemUnionParam, 0, len(m.Thinking)+1+len(m.ToolCalls))
	for _, tb := range m.Thinking {
		if tb.Signature == "" {
			continue
		}
		// Summary is required on the wire even when there is nothing to summarize, and
		// nothing to summarize is the ordinary case: a turn that never asked for a
		// summary still returns reasoning items, carrying encrypted content and an
		// empty summary. Start from an empty slice rather than nil, because the encoder
		// drops a nil one instead of sending [] — the server then sees the field
		// missing and refuses the request with "Missing required parameter:
		// 'input[N].summary'", which fails the turn after any tool call.
		item := responses.ResponseReasoningItemParam{
			ID:      tb.Signature,
			Summary: []responses.ResponseReasoningItemSummaryParam{},
		}
		if tb.Text != "" {
			item.Summary = []responses.ResponseReasoningItemSummaryParam{{Text: tb.Text}}
		}
		if len(tb.Redacted) > 0 {
			item.EncryptedContent = param.NewOpt(string(tb.Redacted))
		}
		items = append(items, responses.ResponseInputItemUnionParam{OfReasoning: &item})
	}
	if m.Text != "" {
		items = append(items, responses.ResponseInputItemParamOfMessage(
			m.Text, responses.EasyInputMessageRoleAssistant))
	}
	for _, tc := range m.ToolCalls {
		items = append(items, responses.ResponseInputItemParamOfFunctionCall(
			string(tc.Input), tc.ID, tc.Name))
	}
	return items
}

// toTools converts tool definitions to SDK function tools, decoding each JSON
// Schema into the SDK's parameters map.
//
// Strict is off, and has to be: it defaults to on, and on requires every property
// listed in required and additionalProperties false throughout. A flow author
// writes an inputSchema by hand and this connector passes it through verbatim, so
// enforcing a shape the author never agreed to would reject ordinary tools —
// starting with the empty `{"type":"object"}` an agent tool defaults to.
func toTools(tools []core.LLMTool) ([]responses.ToolUnionParam, error) {
	if len(tools) == 0 {
		return nil, nil
	}
	out := make([]responses.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		params := map[string]any{}
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &params); err != nil {
				return nil, fmt.Errorf("llm-openai: tool %q input schema: %w", t.Name, err)
			}
		}
		fn := responses.FunctionToolParam{
			Name:       t.Name,
			Parameters: params,
			Strict:     param.NewOpt(false),
		}
		if t.Description != "" {
			fn.Description = param.NewOpt(t.Description)
		}
		out = append(out, responses.ToolUnionParam{OfFunction: &fn})
	}
	return out, nil
}

// toToolChoice maps the agnostic tool choice to the SDK union. The second return
// is false for the auto (zero) mode, signalling the caller to leave it unset.
func toToolChoice(tc core.LLMToolChoice) (responses.ResponseNewParamsToolChoiceUnion, bool) {
	switch tc.Mode {
	case core.LLMToolChoiceAny:
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsRequired),
		}, true
	case core.LLMToolChoiceNone:
		return responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsNone),
		}, true
	case core.LLMToolChoiceTool:
		return responses.ResponseNewParamsToolChoiceUnion{
			OfFunctionTool: &responses.ToolChoiceFunctionParam{Name: tc.Name},
		}, true
	default:
		return responses.ResponseNewParamsToolChoiceUnion{}, false
	}
}

// translateResponse folds the output items into the agnostic response, collecting
// reasoning, text and function calls and mapping the stop reason.
func translateResponse(resp *responses.Response, configuredModel string) (*core.LLMResponse, error) {
	if resp == nil {
		return nil, fmt.Errorf("llm-openai: no response")
	}
	if resp.Error.Message != "" {
		return nil, fmt.Errorf("llm-openai: %s", resp.Error.Message)
	}

	var (
		text     strings.Builder
		calls    []core.LLMToolCall
		thinking []core.LLMThinkingBlock
		refused  bool
	)
	for _, item := range resp.Output {
		switch item.Type {
		case "reasoning":
			// Reasoning, not the answer. Keeping it out of Text is what stops a caller
			// that folds the answer into a message body from publishing it.
			thinking = append(thinking, core.LLMThinkingBlock{
				Text:      summaryText(item.Summary),
				Signature: item.ID,
				Redacted:  []byte(item.EncryptedContent),
			})
		case "message":
			for _, part := range item.Content {
				switch part.Type {
				case "output_text":
					text.WriteString(part.Text)
				case "refusal":
					refused = true
				}
			}
		case "function_call":
			var input json.RawMessage
			if item.Arguments != "" {
				input = json.RawMessage(item.Arguments)
			}
			calls = append(calls, core.LLMToolCall{ID: item.CallID, Name: item.Name, Input: input})
		}
	}

	out := &core.LLMResponse{
		Text:       text.String(),
		ToolCalls:  calls,
		StopReason: mapStopReason(resp, len(calls) > 0, refused),
		Usage:      translateUsage(resp.Usage),
		Model:      servedBy(resp.Model, configuredModel),
	}
	out.Raw = core.LLMMessage{
		Role:      core.LLMRoleAssistant,
		Text:      out.Text,
		Thinking:  thinking,
		ToolCalls: calls,
	}
	return out, nil
}

// summaryText joins a reasoning item's summary parts, which arrive split the same
// way the stream delivers them.
func summaryText(parts []responses.ResponseReasoningItemSummary) string {
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
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

// translateUsage converts the SDK's token counts, reporting nil when the response
// carried none. OutputTokens already counts reasoning tokens inside itself,
// matching the inclusive convention core.LLMUsage adopts.
func translateUsage(u responses.ResponseUsage) *core.LLMUsage {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return nil
	}
	return &core.LLMUsage{
		InputTokens:    int(u.InputTokens),
		OutputTokens:   int(u.OutputTokens),
		ThinkingTokens: int(u.OutputTokensDetails.ReasoningTokens),
		CachedTokens:   int(u.InputTokensDetails.CachedTokens),
		// InputTokens is already the whole prompt — cached reads are a breakdown of
		// it, not a figure beside it — so the two are the same here.
		PromptTokens: int(u.InputTokens),
	}
}

// mapStopReason maps the response's status to the agnostic stop reason.
//
// Responses reports no per-turn finish reason: a turn that wants tools simply ends
// completed with function_call items in its output, so the presence of calls is
// what says tool_use — the same shape Gemini has, and the opposite of Chat
// Completions, which named the reason outright.
func mapStopReason(resp *responses.Response, hasCalls, refused bool) core.LLMStopReason {
	if refused {
		return core.LLMStopRefusal
	}
	if hasCalls {
		return core.LLMStopToolUse
	}
	switch resp.IncompleteDetails.Reason {
	case "max_output_tokens":
		return core.LLMStopMaxTokens
	case "content_filter":
		return core.LLMStopRefusal
	default:
		return core.LLMStopEndTurn
	}
}
