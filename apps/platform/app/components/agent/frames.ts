/**
 * The frames the agent writes to the chat stream — the vocabulary, with no parser
 * in it.
 *
 * The shapes mirror the runtime's agent event vocabulary
 * (`runtime/core/internal/engine/agentevents.go`): the agent's `events` path sends
 * each one straight to the caller's stream, so what the browser reads is the event
 * body with its `type` intact. Two frames do not come from there: `navigate`, which
 * the agent's own tool emits, and the route's closing `answer`.
 *
 * Separate from the parsing beside it because this is the contract and that is the
 * distrust of it — and because the reducer imports these and nothing else.
 */

/** One parsed server-sent event: its `event:` name and its `data:` payload. */
export interface SSEFrame {
  event: string;
  data: string;
}

/**
 * What every frame carries besides its own fields.
 *
 * `iteration` is the agent's own turn counter, stamped on every event it reports.
 * It is what tells two rounds of tool calls apart when nothing was said between
 * them — without it they read as one long list, and the fact that the agent went
 * back to the model, which is the expensive part, is invisible.
 */
export interface FrameCommon {
  iteration?: number;
}

/** A token of the answer as it is generated. */
export interface TextEvent extends FrameCommon {
  type: "text";
  text: string;
  index?: number;
}

/**
 * A token of the model's reasoning, before it commits to an answer.
 *
 * Usually most of what a run produces — on a reasoning model the thinking can be
 * an order of magnitude longer than the reply — so this is what fills the time
 * between the question and the first word of the answer.
 */
export interface ThinkingEvent extends FrameCommon {
  type: "thinking";
  text: string;
  index?: number;
}

/** The model asking for a tool, with its arguments complete. */
export interface ToolCallEvent extends FrameCommon {
  type: "tool_call";
  tool: string;
  toolCallId: string;
  input?: unknown;
}

/**
 * A tool call the agent is holding in front of a person before it runs.
 *
 * It arrives after the `tool_call` it is about, so the panel already has the call
 * on screen and this is the question attached to it. `input` is the arguments as
 * the model asked for them — what is being authorized is this call, not the tool
 * in general — and `authorizationId` is what an answer quotes.
 *
 * Nobody has to answer. `expiresInSeconds` is how long the run will wait before
 * denying on their behalf, so the panel can show the clock the runtime is
 * actually running rather than one of its own.
 */
export interface ToolAuthorizationEvent extends FrameCommon {
  type: "tool_authorization";
  tool: string;
  toolCallId: string;
  authorizationId: string;
  input?: unknown;
  expiresInSeconds?: number;
}

/** What the flow branch that ran a tool returned. */
export interface ToolResultEvent extends FrameCommon {
  type: "tool_result";
  tool: string;
  toolCallId: string;
  output?: unknown;
  isError?: boolean;
}

/** The agent finishing with an answer. */
export interface DoneEvent extends FrameCommon {
  type: "done";
  text?: string;
}

/** A model call that failed. */
export interface ErrorEvent extends FrameCommon {
  type: "error";
  error: string;
}

/** The agent declining the question and taking its guardrail. */
export interface GuardrailEvent extends FrameCommon {
  type: "guardrail";
  reason?: string;
}

/**
 * A finished model turn, and with it the exact size of the conversation.
 *
 * The gauge is measured rather than estimated — what the provider read plus what
 * it produced — and it arrives on every turn, so a panel can show the context
 * filling up instead of only reporting the overflow afterwards. `contextMaxTokens`
 * is absent for an agent with no budget, and the pair is useless apart: 12,000 on
 * its own says nothing about whether the next turn will fit.
 */
export interface TurnEndEvent extends FrameCommon {
  type: "turn_end";
  contextTokens?: number;
  contextMaxTokens?: number;
}

/**
 * The agent shrinking its own conversation to stay inside its budget.
 *
 * Two events rather than one because the summarize strategy makes a real model
 * call: compaction can take seconds, and a panel with only an after-the-fact
 * report shows a stall it cannot explain.
 */
export interface CompactionStartEvent extends FrameCommon {
  type: "compaction_start";
  strategy?: string;
}

export interface CompactionEndEvent extends FrameCommon {
  type: "compaction_end";
  dropped?: number;
}

/**
 * Something posted to the run from outside it: a message handed over while it was
 * answering, or one it accepted and never got a turn to answer.
 *
 * It is the only event describing an instruction the agent did not derive from
 * its own work, which is exactly why it is worth showing — a follow-up that
 * changed the answer is otherwise invisible in the transcript.
 */
export interface SignalEvent extends FrameCommon {
  type: "signal";
  signal: string;
  text?: string;
  /** An `authorize` signal carries the decision instead of text. */
  authorizationId?: string;
  allowed?: boolean;
}

/**
 * The conversation being named, which happens once — on the run that opened it,
 * after the answer has already streamed.
 *
 * Without it a panel only learns titles from a listing, so the conversation
 * somebody is *in* stays nameless until something reloads it.
 */
export interface ThreadTitleEvent extends FrameCommon {
  type: "thread_title";
  title: string;
  /** Which conversation was named; a caller may be watching more than one. */
  thread?: string;
}

export type AgentEvent =
  | TextEvent
  | ThinkingEvent
  | ToolCallEvent
  | ToolAuthorizationEvent
  | ToolResultEvent
  | TurnEndEvent
  | CompactionStartEvent
  | CompactionEndEvent
  | SignalEvent
  | ThreadTitleEvent
  | DoneEvent
  | ErrorEvent
  | GuardrailEvent;

/** Where the agent wants to take the user, and why. */
export interface NavigateEvent {
  path: string;
  reason?: string;
}
