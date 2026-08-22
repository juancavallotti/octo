// Package openrouter provides the "llm-openrouter" connector: a configured
// OpenRouter client that satisfies core.LLMClient so the AI flow elements can
// drive it interchangeably with the other providers. It translates the
// provider-agnostic core.LLM* DTOs to and from the wire types on each Complete
// call.
//
// OpenRouter fronts hundreds of models from many vendors behind one key and one
// API, so a flow points at `anthropic/claude-sonnet-4.5` or `openai/gpt-5.4`
// without a connector and an account per vendor. The model id carries the vendor
// as a prefix; everything else about configuring this connector is the same
// shape as the other three.
//
// It speaks Chat Completions rather than Responses, which is the opposite choice
// from llm-openai and made for the opposite reason. OpenRouter's Responses
// endpoint is beta and does not cover every upstream it routes to, while Chat
// Completions is the surface every routed model answers on. The conflict that
// drove llm-openai off Chat Completions — a reasoning effort that could not be
// combined with function tools — is OpenAI's, not OpenRouter's: reasoning here
// is a request field of OpenRouter's own, and the reasoning that comes back
// rides on the message rather than being absent.
//
// Requests are stateless: the whole conversation is sent every turn, matching
// the other three connectors and keeping nothing on the provider's side. That is
// why an assistant turn echoes its reasoning_details verbatim — with nothing
// stored upstream, the echoed block is what lets a thinking model continue the
// same train of thought across a tool call.
package openrouter

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
	"github.com/openai/openai-go/v2/shared"

	"github.com/juancavallotti/octo/runtime/core"
	"github.com/juancavallotti/octo/runtime/types"
)

func init() {
	core.MustRegisterConnector("llm-openrouter", func() core.Connector {
		return &Connector{}
	})

	core.RegisterConnectorMeta(core.ConnectorMeta{
		Type:     "llm-openrouter",
		Label:    "OpenRouter",
		Icon:     "Sparkles",
		Category: "llm",
		Settings: reflect.TypeFor[connectorSettings](),
	})
}

const (
	// defaultModel is a vendor-prefixed OpenRouter id, which is the whole
	// namespace this connector addresses: there is no bare model name here.
	defaultModel = "anthropic/claude-sonnet-4.5"

	// defaultBaseURL is OpenRouter's API root. It is a default rather than a
	// constant so a test can point the connector at an httptest server and an
	// operator at a proxy.
	defaultBaseURL = "https://openrouter.ai/api/v1"

	// reasoningNone asks for no reasoning at all; reasoningDefault sends no
	// reasoning field and lets the model apply its own.
	reasoningNone    = "none"
	reasoningDefault = "default"

	// The headers OpenRouter attributes traffic with. Both are optional and only
	// sent when configured, because an empty one is worse than none — it appears
	// in OpenRouter's dashboards as an app with no name.
	headerReferer = "HTTP-Referer"
	headerTitle   = "X-Title"

	// jsonNull is an explicit null on the wire, which is a different fact from an
	// omitted field but the same instruction here: there is nothing to decode.
	jsonNull = "null"
)

// connectorSettings is the configuration decoded from the connector's settings.
type connectorSettings struct {
	// Authenticates with the OpenRouter API; source from ${OPENROUTER_API_KEY}. Never logged.
	APIKey string `json:"apiKey" octo:"label=API key,required"`
	// Model id, vendor-prefixed as OpenRouter publishes it (e.g. openai/gpt-5.4).
	Model string `json:"model" octo:"label=Model,default=anthropic/claude-sonnet-4.5"`
	// Default response token cap (0 = the model default); a request may override it.
	MaxTokens int `json:"maxTokens" octo:"label=Max tokens"`
	// How much reasoning effort a reasoning-capable model spends. "default" sends no
	// reasoning field at all and lets the model choose, which is the only setting that
	// is also safe on a model with no reasoning to configure.
	//nolint:lll // a struct tag cannot be wrapped, and the enum has to list every option
	Reasoning string `json:"reasoning" octo:"label=Reasoning,type=enum,enum=default|none|minimal|low|medium|high,default=default"`
	// Overrides the API endpoint (for proxies or testing).
	BaseURL string `json:"baseURL" octo:"label=Base URL"`
	// Optional app name OpenRouter attributes this traffic to, sent as X-Title.
	AppName string `json:"appName" octo:"label=App name"`
	// Optional site URL OpenRouter attributes this traffic to, sent as HTTP-Referer.
	SiteURL string `json:"siteURL" octo:"label=Site URL"`
}

// Connector is a configured OpenRouter client that AI elements call through. It
// is safe for concurrent use: the SDK client is, and the connector holds only
// immutable configuration after Start.
type Connector struct {
	client    sdk.Client
	model     string
	maxTokens int
	reasoning string
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
		return fmt.Errorf("llm-openrouter connector %q: apiKey is required", config.Name)
	}

	c.model = set.Model
	if c.model == "" {
		c.model = defaultModel
	}
	c.maxTokens = set.MaxTokens

	reasoning, err := toReasoningEffort(set.Reasoning)
	if err != nil {
		return fmt.Errorf("llm-openrouter connector %q: %w", config.Name, err)
	}
	c.reasoning = reasoning

	baseURL := set.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	opts := []option.RequestOption{
		option.WithAPIKey(set.APIKey),
		option.WithBaseURL(baseURL),
	}
	if set.SiteURL != "" {
		opts = append(opts, option.WithHeader(headerReferer, set.SiteURL))
	}
	if set.AppName != "" {
		opts = append(opts, option.WithHeader(headerTitle, set.AppName))
	}
	c.client = sdk.NewClient(opts...)

	slog.Info("llm-openrouter connector started",
		"connector", config.Name,
		"model", c.model,
		"maxTokens", c.maxTokens,
		"reasoning", set.Reasoning,
	)
	return nil
}

// toReasoningEffort validates the setting at startup. It returns the setting as
// written rather than a wire value, because the wire shape is an object built in
// reasoningField and the empty string there means "send nothing".
//
// Unset means default, and default means silent. OpenRouter routes to models with
// no reasoning to configure at all, so sending the field unasked is how a
// connector pointed at one of those fails on every call. Saying nothing works
// everywhere and lets a reasoning model apply the effort it was trained to.
func toReasoningEffort(effort string) (string, error) {
	switch effort {
	case "", reasoningDefault:
		return "", nil
	case reasoningNone, string(shared.ReasoningEffortMinimal), string(shared.ReasoningEffortLow),
		string(shared.ReasoningEffortMedium), string(shared.ReasoningEffortHigh):
		return effort, nil
	default:
		return "", fmt.Errorf(
			"reasoning must be one of default, none, minimal, low, medium, high, got %q", effort)
	}
}

// Stop is a no-op: the connector holds no resources to release.
func (c *Connector) Stop(context.Context) error { return nil }

// Provider names the vendor family, in the vocabulary the price catalogue uses.
//
// It is OPENROUTER rather than the vendor behind whichever model answered, and
// that is the point: the figure downstream depends on whose token accounting
// produced the counts, and these are OpenRouter's — an input count that already
// includes the tokens served from cache, the OpenAI-compatible convention, even
// when the model behind it is an Anthropic one that would have reported the
// uncached remainder on its own API.
func (c *Connector) Provider() string { return core.ProviderOpenRouter }

// Complete runs one Chat Completions turn, translating the request to SDK params
// and the response back to the provider-agnostic DTOs.
func (c *Connector) Complete(ctx context.Context, req core.LLMRequest) (*core.LLMResponse, error) {
	params, err := c.params(req, false)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("llm-openrouter complete: %w", err)
	}
	t, err := turnFromCompletion(resp)
	if err != nil {
		return nil, err
	}
	return translateTurn(t, c.model), nil
}

// Stream runs one turn over the streaming endpoint, reporting content as it
// arrives and returning the same response Complete would have.
//
// The two paths meet at translateTurn: the fold below gathers exactly the fields
// turnFromCompletion gathers, so a streamed turn and a blocking one cannot
// disagree about what the turn contained. Chat Completions has no terminal object
// carrying the finished response, so unlike llm-openai there is a fold here at
// all — but it folds into the same intermediate rather than into a second
// translation.
func (c *Connector) Stream(
	ctx context.Context, req core.LLMRequest, on func(core.LLMStreamEvent) error,
) (*core.LLMResponse, error) {
	params, err := c.params(req, true)
	if err != nil {
		return nil, err
	}

	stream := c.client.Chat.Completions.NewStreaming(ctx, params)
	defer func() { _ = stream.Close() }()

	t, err := foldStream(stream, on)
	if err != nil {
		return nil, err
	}
	return translateTurn(t, c.model), nil
}

// params builds the SDK request shared by Complete and Stream, so the two paths
// cannot drift in what they ask the model for. Only stream_options differs, and
// only because the API rejects it on a request that is not streaming.
func (c *Connector) params(req core.LLMRequest, streaming bool) (sdk.ChatCompletionNewParams, error) {
	messages, err := toMessages(req.System, req.Messages)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, err
	}
	tools, err := toTools(req.Tools)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, err
	}

	params := sdk.ChatCompletionNewParams{
		Model:    c.model,
		Messages: messages,
	}
	maxTokens := c.maxTokens
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}
	if maxTokens > 0 {
		params.MaxTokens = param.NewOpt(int64(maxTokens))
	}
	if len(tools) > 0 {
		params.Tools = tools
		if choice, ok := toToolChoice(req.ToolChoice); ok {
			params.ToolChoice = choice
		}
	}
	if streaming {
		// Without this the usage object never arrives on a streamed turn, and a
		// turn whose cost is unknown is not the same turn Complete would have
		// returned.
		params.StreamOptions = sdk.ChatCompletionStreamOptionsParam{
			IncludeUsage: param.NewOpt(true),
		}
	}

	// The two request fields the OpenAI schema has no room for. Usage accounting
	// is asked for on every call because it is what carries the cost OpenRouter
	// charged, and a cost nobody asked for is a cost nothing downstream can report.
	extras := map[string]any{"usage": map[string]any{"include": true}}
	if field, ok := reasoningField(c.reasoning); ok {
		extras["reasoning"] = field
	}
	params.SetExtraFields(extras)

	return params, nil
}

// reasoningField renders the configured effort as OpenRouter's reasoning object.
// The second return is false for the default, signalling that the request should
// carry no reasoning field at all.
func reasoningField(effort string) (map[string]any, bool) {
	switch effort {
	case "":
		return nil, false
	case reasoningNone:
		// Switched off explicitly, which is a different instruction from saying
		// nothing: a model that reasons by default stops.
		return map[string]any{"enabled": false}, true
	default:
		return map[string]any{"effort": effort}, true
	}
}

// Embed embeds a batch of texts in one request, translating the SDK's
// index-tagged results back into request order — the SDK documents input as an
// array but doesn't promise the response preserves that order.
//
// Embedding model ids are vendor-prefixed here like every other model OpenRouter
// serves (openai/text-embedding-3-small, google/gemini-embedding-001), and the
// block names the model, so nothing here defaults one.
func (c *Connector) Embed(ctx context.Context, req core.EmbedRequest) (*core.EmbedResponse, error) {
	if len(req.Input) == 0 {
		return nil, fmt.Errorf("llm-openrouter embed: input is required")
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
		return nil, fmt.Errorf("llm-openrouter embed: %w", err)
	}
	return translateEmbedResponse(resp, len(req.Input), req.Model)
}

// decodeExtra reads one of the fields OpenRouter adds beyond the OpenAI schema.
// A field the SDK does not know is still on the wire, and this is where it is
// taken off it.
func decodeExtra(raw string, into any) bool {
	if raw == "" || raw == jsonNull {
		return false
	}
	return json.Unmarshal([]byte(raw), into) == nil
}
