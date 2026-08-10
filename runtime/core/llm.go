package core

import (
	"context"
	"encoding/json"
)

// LLMClient is the provider-agnostic capability the AI elements (ai-router,
// ai-agent, ai-mapping, ai-retry) depend on. The provider connectors
// (llm-anthropic, llm-openai, llm-gemini) satisfy it and translate these DTOs
// to and from their SDK types.
//
// The interface lives in core (not a connector package) on purpose: the AI
// composites are built by the flow builder in core/internal/engine, and core
// cannot import the connector packages without a cycle. So an AI element resolves
// a connector by name through BlockDeps.Connector and type-asserts the result to
// LLMClient — the interface, not a concrete connector type. This is the
// deliberate divergence from the concrete-type assertion other blocks use (e.g.
// the rest block asserting *httpclient.Connector), forced by the requirement that
// an AI element work with any provider.
//
// Implementations must be safe for concurrent use: one connector instance is
// shared across all flows that reference it.
type LLMClient interface {
	// Complete runs a single chat/completion turn. The request carries the full
	// conversation so far (system + messages) and any tool definitions; the
	// response is either assistant text, a set of tool calls the model wants run,
	// or both. Callers drive multi-turn tool loops by appending the assistant
	// turn (LLMResponse.Raw) and the tool results, then calling again.
	Complete(ctx context.Context, req LLMRequest) (*LLMResponse, error)
}

// LLMProvider is the optional half of a provider connector that names the vendor
// family behind it. Callers type-assert for it exactly as they do for
// LLMStreamClient and EmbedClient, so a connector that does not implement it
// still works — its calls are simply recorded without a provider.
//
// It exists because the connector's configured *name* is the author's ("my-llm"),
// not the vendor's, and the model id is not a reliable substitute: "gpt-4o" is
// published under both OPENAI and AZURE, and a newly shipped Anthropic model can
// match a BEDROCK pattern before the price catalogue lists it under ANTHROPIC.
// Guessing wrong is not cosmetic — the provider decides how cached tokens are
// counted, so it decides the cost.
type LLMProvider interface {
	// Provider returns the vendor family that serves this connector's calls, in
	// the vocabulary the price catalogue uses: ANTHROPIC, OPENAI, GOOGLE.
	//
	// The family, not the endpoint: a connector pointed at a proxy or an
	// OpenAI-compatible server still reports the family whose API and token
	// accounting it speaks, because that is what the number downstream depends on.
	Provider() string
}

// The vendor families the bundled connectors report. They are named here rather
// than spelled at each connector so the vocabulary a cost reader parses is
// enumerable from one place — and so the one that surprises people, Gemini's
// vendor being GOOGLE, is written down once.
const (
	ProviderAnthropic = "ANTHROPIC"
	ProviderOpenAI    = "OPENAI"
	ProviderGoogle    = "GOOGLE"
)

// LLMStreamClient is the optional streaming half of a provider. A connector that
// implements it can report a turn's output as it is produced instead of only when
// it is finished. Callers type-assert for it exactly as they do for EmbedClient,
// and fall back to Complete when a provider does not have it.
type LLMStreamClient interface {
	// Stream runs one completion turn, calling on for each event as it arrives, and
	// returns the same *LLMResponse Complete would have returned for that turn.
	//
	// The events are strictly additive: everything a caller needs is still on the
	// returned response, so moving a call from Complete to Stream changes when the
	// caller learns things, never what it learns. That is what makes streaming safe
	// to switch on.
	//
	// on is called on the calling goroutine, in order, between reads of the
	// provider's connection — so a slow handler backpressures the model rather than
	// buffering without bound. Returning an error from on stops the stream and is
	// returned as-is, which is how a caller abandons a turn nobody is listening to.
	Stream(ctx context.Context, req LLMRequest, on func(LLMStreamEvent) error) (*LLMResponse, error)
}

// LLMStreamKind is the canonical vocabulary for a streamed event.
//
// It is a closed set on purpose. Every provider maps its own wire events onto
// these, and anything with no canonical home goes to LLMStreamCustom rather than
// growing the list. Two rules keep that honest: a provider never uses custom for
// something a canonical kind already covers, and never synthesizes a kind it does
// not actually have.
//
// Consumers must tolerate any kind being absent, because the gaps between
// providers are real and permanent rather than bugs waiting to be fixed. Today
// the only such gap is thinking on OpenAI, whose Chat Completions API exposes no
// reasoning content at all — not on a delta, not on the finished message.
//
// Granularity is not guaranteed either, only meaning. Gemini delivers a tool
// call's arguments whole where the other two fragment them, so it reports one
// tool_input rather than several; a consumer that concatenates the fragments of a
// call gets the same valid JSON on all three.
//
// There is deliberately no terminal kind. Stream returns the finished
// LLMResponse, so stop reason, usage and the assembled text are all read from
// there — a "done" event would be a second, weaker way to learn the same thing.
type LLMStreamKind string

const (
	// LLMStreamText is a fragment of the assistant's answer.
	LLMStreamText LLMStreamKind = "text"
	// LLMStreamThinking is a fragment of the model's reasoning. A provider emits it
	// only when reasoning was both requested and is returned as content.
	LLMStreamThinking LLMStreamKind = "thinking"
	// LLMStreamToolInput is a fragment of one tool call's argument JSON. The
	// fragments are not individually parseable — only their concatenation is.
	LLMStreamToolInput LLMStreamKind = "tool_input"
	// LLMStreamCustom is a provider event with no canonical equivalent, carried
	// through under the provider's own name for it. It is also what a connector
	// falls back to for an event its SDK has grown since the connector was written,
	// so a new provider event reaches consumers without the vocabulary changing.
	LLMStreamCustom LLMStreamKind = "custom"
)

// LLMStreamEvent is one event from a streamed turn. Which fields are populated
// depends on Kind; a consumer that does not recognize a Kind can always ignore
// the event, since the finished response carries everything it needs.
type LLMStreamEvent struct {
	Kind LLMStreamKind
	// Name is the provider's own name for a custom event. Empty for every canonical
	// kind.
	Name string
	// Text is the fragment for text, thinking and tool_input. For custom it is the
	// raw provider payload.
	Text string
	// Tool and ToolCallID identify the call a tool_input fragment belongs to. They
	// are set from the point the provider names the call, which for a fragmented
	// tool call is its first fragment.
	Tool       string
	ToolCallID string
	// Index is the provider's content-block index. Blocks interleave, so this is
	// what distinguishes two runs of fragments arriving at once.
	//
	// It is unique only within a Kind. A provider with no content-block index of
	// its own numbers its tool calls from zero while leaving text at zero too, so
	// a consumer grouping fragments keys on Kind and Index together — Index alone
	// would fold a text run into the first tool call.
	Index int
}

// LLMRequest is one completion turn. The shape mirrors the Anthropic Messages
// tool-use loop (system separate from the conversation, explicit tool-call IDs,
// tool results as their own turn) because it is the most expressive of the three
// providers and maps cleanly onto OpenAI and Gemini.
type LLMRequest struct {
	// System is the system prompt. It is provider-routed to the dedicated
	// system slot rather than prepended as a message. May be empty.
	System string
	// Messages is the ordered conversation: user turns, prior assistant turns
	// (which may carry ToolCalls), and tool turns (which carry ToolResults).
	Messages []LLMMessage
	// Tools are the function definitions the model may call. May be empty for a
	// plain text completion (e.g. ai-mapping).
	Tools []LLMTool
	// ToolChoice constrains whether and which tool the model must call. The zero
	// value is auto (the model decides).
	ToolChoice LLMToolChoice
	// MaxTokens caps the response length. Zero means the connector's default.
	MaxTokens int
}

// LLMRole identifies who produced a message.
type LLMRole string

const (
	// LLMRoleUser is an end-user / caller turn.
	LLMRoleUser LLMRole = "user"
	// LLMRoleAssistant is a model turn; it may carry ToolCalls.
	LLMRoleAssistant LLMRole = "assistant"
	// LLMRoleTool is a turn carrying the results of tool calls (ToolResults).
	LLMRoleTool LLMRole = "tool"
)

// LLMMessage is one turn in the conversation. Which fields are populated depends
// on Role: user/assistant turns carry Text, an assistant turn may also carry
// Thinking and ToolCalls, and a tool turn carries ToolResults.
type LLMMessage struct {
	Role LLMRole
	Text string
	// Thinking is the assistant turn's reasoning blocks, in the order the provider
	// produced them. See LLMThinkingBlock: this exists for correctness, not
	// observability, and callers driving a tool loop must carry it back untouched.
	Thinking    []LLMThinkingBlock
	ToolCalls   []LLMToolCall
	ToolResults []LLMToolResult
}

// LLMThinkingBlock is one reasoning block from an assistant turn.
//
// It is carried on the message rather than dropped because a provider may
// require it back. With extended thinking enabled, Anthropic validates the
// thinking runs of an echoed assistant turn and rejects a request whose blocks
// were dropped, reordered, or edited — so a tool loop that discards them breaks
// on the second turn. Callers treat these blocks as opaque: they never inspect,
// merge, or construct one, they only carry it back via LLMResponse.Raw.
//
// Exactly one of Text or Redacted is set. A redacted block is reasoning the
// provider encrypted rather than returned; it must still be echoed verbatim.
type LLMThinkingBlock struct {
	// Text is the reasoning content. Empty for a redacted block.
	Text string
	// Signature is the provider's attestation over Text. It is verified against the
	// exact bytes of Text, so changing either invalidates the block.
	Signature string
	// Redacted is the opaque payload of a redacted block, echoed back as-is.
	Redacted []byte
}

// LLMUsage is the token accounting for one completion turn. A provider that does
// not report a given figure leaves it zero; LLMResponse.Usage is nil when the
// provider reported nothing at all.
//
// OutputTokens is defined as the billing-authoritative total and therefore
// *includes* ThinkingTokens. Providers disagree here — Anthropic and OpenAI
// already count reasoning inside their output total, Gemini reports thoughts
// separately — so the connectors normalize to the inclusive figure and callers
// never have to know which provider answered.
type LLMUsage struct {
	InputTokens    int
	OutputTokens   int
	ThinkingTokens int
	CachedTokens   int
}

// LLMTool is a function the model may call. InputSchema is a JSON Schema object
// describing the arguments; it is passed through to the provider verbatim.
type LLMTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// LLMToolChoiceMode selects the tool-calling policy for a request.
type LLMToolChoiceMode string

const (
	// LLMToolChoiceAuto lets the model decide whether to call a tool. Zero value.
	LLMToolChoiceAuto LLMToolChoiceMode = ""
	// LLMToolChoiceAny forces the model to call some tool (its choice which).
	LLMToolChoiceAny LLMToolChoiceMode = "any"
	// LLMToolChoiceNone forbids tool calls.
	LLMToolChoiceNone LLMToolChoiceMode = "none"
	// LLMToolChoiceTool forces the model to call the tool named in
	// LLMToolChoice.Name.
	LLMToolChoiceTool LLMToolChoiceMode = "tool"
)

// LLMToolChoice constrains tool calling. Name is used only when Mode is
// LLMToolChoiceTool.
type LLMToolChoice struct {
	Mode LLMToolChoiceMode
	Name string
}

// LLMToolCall is a request from the model to run a tool. ID correlates the call
// with its later LLMToolResult. Providers that do not supply IDs (Gemini) have
// their connector synthesize a stable one. Input is the arguments as a JSON
// object.
type LLMToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
	// Signature is an opaque, provider-specific continuation token the model
	// attaches to a tool call that must be echoed back verbatim on the next turn
	// for a multi-turn tool conversation to stay valid (Gemini 3.x thought
	// signatures). It is empty for providers that do not use one; callers treat it
	// as opaque and never inspect or construct it — they only carry it back via
	// LLMResponse.Raw.
	Signature []byte
}

// LLMToolResult is the outcome of a tool call fed back to the model. ToolCallID
// must match the originating LLMToolCall.ID. Content is the serialized result;
// IsError marks it as a failure the model should react to rather than an answer.
type LLMToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}

// LLMStopReason is why the model stopped generating.
type LLMStopReason string

const (
	// LLMStopEndTurn is a normal completion.
	LLMStopEndTurn LLMStopReason = "end_turn"
	// LLMStopToolUse means the model wants tools run; ToolCalls is populated.
	LLMStopToolUse LLMStopReason = "tool_use"
	// LLMStopMaxTokens means the response hit the token cap.
	LLMStopMaxTokens LLMStopReason = "max_tokens"
	// LLMStopRefusal means the model declined to answer.
	LLMStopRefusal LLMStopReason = "refusal"
)

// LLMResponse is the result of one completion turn. Text is the assembled text
// output; ToolCalls is set when StopReason is LLMStopToolUse. Raw is the
// assistant turn as an LLMMessage, ready to append back onto LLMRequest.Messages
// when driving a tool loop.
//
// Text carries the model's answer only. Reasoning never appears in it — it lives
// in Raw.Thinking, so a caller that folds Text into a message body cannot leak
// chain-of-thought into user-visible output.
type LLMResponse struct {
	Text       string
	ToolCalls  []LLMToolCall
	StopReason LLMStopReason
	Raw        LLMMessage
	// Usage is the turn's token accounting, or nil when the provider reported none.
	Usage *LLMUsage
	// Model is the model that actually served the turn, as the provider reported
	// it, falling back to the id the connector was configured with when it
	// reported none.
	//
	// The two differ more often than not, and the difference is the point: a
	// configured alias resolves to a dated snapshot, and it is the snapshot that
	// answered and the snapshot that is billed. Pairing Usage with the alias
	// would attribute tokens to something that does not have a price.
	Model string
}
