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
// ToolCalls, and a tool turn carries ToolResults.
type LLMMessage struct {
	Role        LLMRole
	Text        string
	ToolCalls   []LLMToolCall
	ToolResults []LLMToolResult
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
type LLMResponse struct {
	Text       string
	ToolCalls  []LLMToolCall
	StopReason LLMStopReason
	Raw        LLMMessage
}
