package core

import (
	"context"
	"encoding/json"
)

// LLMClient is the provider-agnostic completion capability. A provider connector
// satisfies it and translates these DTOs to and from its SDK types.
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
// family behind it. A connector that does not implement it still works — its
// calls are recorded without a provider.
//
// A connector's configured name is the flow author's ("my-llm"), not the vendor's,
// and the model id is not a reliable substitute: "gpt-4o" is published under both
// OPENAI and AZURE. The vendor family decides how cached tokens are counted, so
// it decides the cost.
type LLMProvider interface {
	// Provider returns the vendor family that serves this connector's calls:
	// ANTHROPIC, OPENAI, GOOGLE.
	//
	// The family, not the endpoint: a connector pointed at a proxy or an
	// OpenAI-compatible server reports the family whose API and token accounting
	// it speaks.
	Provider() string
}

// The vendor families a provider connector reports. Gemini's family is GOOGLE.
const (
	ProviderAnthropic  = "ANTHROPIC"
	ProviderOpenAI     = "OPENAI"
	ProviderGoogle     = "GOOGLE"
	ProviderOpenRouter = "OPENROUTER"
)

// LLMStreamClient is the optional streaming half of a provider. A connector that
// implements it can report a turn's output as it is produced instead of only when
// it is finished; one that does not is driven through Complete.
type LLMStreamClient interface {
	// Stream runs one completion turn, calling on for each event as it arrives, and
	// returns the same *LLMResponse Complete would have returned for that turn.
	//
	// The events are strictly additive: everything a caller needs is still on the
	// returned response, so streaming changes when the caller learns things, never
	// what it learns.
	//
	// on is called on the calling goroutine, in order, between reads of the
	// provider's connection — so a slow handler backpressures the model rather than
	// buffering without bound. Returning an error from on stops the stream and is
	// returned as-is.
	Stream(ctx context.Context, req LLMRequest, on func(LLMStreamEvent) error) (*LLMResponse, error)
}

// LLMStreamKind is the canonical vocabulary for a streamed event. It is a closed
// set: a provider maps its own wire events onto these, sends anything with no
// canonical home as LLMStreamCustom, and never synthesizes a kind it does not
// actually produce.
//
// Consumers must tolerate any kind being absent, and must not rely on
// granularity — only on meaning. A provider that delivers a tool call's arguments
// whole reports one tool_input where another reports several, and concatenating
// a call's fragments yields the same valid JSON either way.
//
// There is no terminal kind: stop reason, usage and the assembled text are read
// from the LLMResponse that Stream returns.
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
	// through under the provider's own name for it.
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

// LLMRequest is one completion turn: the system prompt separate from the
// conversation, explicit tool-call IDs, and tool results as their own turn.
type LLMRequest struct {
	// System is the system prompt. It is provider-routed to the dedicated
	// system slot rather than prepended as a message. May be empty.
	System string
	// Messages is the ordered conversation: user turns, prior assistant turns
	// (which may carry ToolCalls), and tool turns (which carry ToolResults).
	Messages []LLMMessage
	// Tools are the function definitions the model may call. May be empty for a
	// plain text completion.
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

// LLMThinkingBlock is one reasoning block from an assistant turn, carried because
// a provider may require it back: a provider that validates the thinking runs of
// an echoed assistant turn rejects a request whose blocks were dropped, reordered
// or edited, so a tool loop that discards them breaks on the second turn.
//
// The blocks are opaque. Callers never inspect, merge or construct one; they
// carry it back via LLMResponse.Raw.
//
// Text and Redacted are not exclusive. A provider may return exactly one of them,
// or both at once — a readable summary alongside the encrypted reasoning the next
// turn has to echo — so a consumer carries whichever it was given.
type LLMThinkingBlock struct {
	// Text is the reasoning content, or a summary of it. Empty when the provider
	// returned no readable reasoning.
	Text string
	// Signature is the token that makes the block echoable: either an attestation
	// over Text, verified against its exact bytes, or the id the server matches the
	// echoed block against.
	Signature string
	// Redacted is the opaque encrypted payload, echoed back as-is.
	Redacted []byte
}

// LLMUsage is the token accounting for one completion turn. A provider that does
// not report a given figure leaves it zero; LLMResponse.Usage is nil when the
// provider reported nothing at all.
//
// OutputTokens is the billing-authoritative total and therefore *includes*
// ThinkingTokens. Providers disagree on this, so a connector normalizes to the
// inclusive figure and callers never have to know which one answered.
//
// CachedTokens and CacheWriteTokens are the two halves of prompt caching and are
// billed differently: a read is cheaper than ordinary input, a write dearer. A
// provider that does not charge separately for a write leaves it zero.
type LLMUsage struct {
	InputTokens      int
	OutputTokens     int
	ThinkingTokens   int
	CachedTokens     int
	CacheWriteTokens int

	// PromptTokens is every token the provider read for this turn — the system
	// prompt, the tool schemas and the whole conversation — counting the ones it
	// served from cache and the ones it wrote to it. It is the only portable
	// measure of how full a context is, and no sum over the fields above
	// reconstructs it: a provider reporting InputTokens as the uncached remainder
	// and one reporting cached reads as a subset of it make the same arithmetic
	// mean two different things.
	//
	// A connector normalizes it, as it does OutputTokens and thinking. It is
	// always >= InputTokens.
	PromptTokens int

	// ReportedCostUSD is the amount the provider says it charged for this turn,
	// and is nil for a provider that reports no such figure.
	//
	// It is relayed, never derived: it is a number the provider volunteered, and
	// it already includes the per-request and per-image charges no token count
	// reconstructs.
	ReportedCostUSD *float64
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
// with its later LLMToolResult; a connector whose provider supplies no id
// synthesizes a stable one. Input is the arguments as a JSON object.
type LLMToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
	// Signature is an opaque continuation token some providers attach to a tool
	// call, which must be echoed back verbatim on the next turn for a multi-turn
	// tool conversation to stay valid. It is empty where the provider uses none;
	// callers never inspect or construct it, they carry it back via
	// LLMResponse.Raw.
	Signature []byte
}

// LLMToolResult is the outcome of a tool call fed back to the model. ToolCallID
// must match the originating LLMToolCall.ID. Content is the serialized result;
// IsError marks it as a failure the model should react to rather than an answer.
type LLMToolResult struct {
	ToolCallID string
	// Tool is the name of the call this answers, carried alongside the id because
	// not every provider correlates on the id alone — some address a function
	// response by name. It must match the originating LLMToolCall.Name.
	Tool    string
	Content string
	IsError bool
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
	// it, falling back to the configured id when it reported none. It is the
	// model Usage belongs to: a configured alias resolves to a dated snapshot, and
	// it is the snapshot that answered and that is billed.
	Model string
}
