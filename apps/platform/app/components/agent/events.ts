/**
 * The frames the agent writes to the chat stream, and a parser for the wire format
 * they arrive in.
 *
 * The shapes mirror the runtime's agent event vocabulary
 * (`runtime/core/internal/engine/agentevents.go`) — the agent's `events` path sends
 * each one straight to the caller's stream, so what the browser reads is the event
 * body with its `type` intact. Two frames do not come from there: `navigate`, which
 * the agent's own tool emits, and the route's closing `answer`.
 */

/** One parsed server-sent event: its `event:` name and its `data:` payload. */
export interface SSEFrame {
  event: string;
  data: string;
}

/** A token of the answer as it is generated. */
export interface TextEvent {
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
export interface ThinkingEvent {
  type: "thinking";
  text: string;
  index?: number;
}

/** The model asking for a tool, with its arguments complete. */
export interface ToolCallEvent {
  type: "tool_call";
  tool: string;
  toolCallId: string;
  input?: unknown;
}

/** What the flow branch that ran a tool returned. */
export interface ToolResultEvent {
  type: "tool_result";
  tool: string;
  toolCallId: string;
  output?: unknown;
  isError?: boolean;
}

/** The agent finishing with an answer. */
export interface DoneEvent {
  type: "done";
  text?: string;
}

/** A model call that failed. */
export interface ErrorEvent {
  type: "error";
  error: string;
}

/** The agent declining the question and taking its guardrail. */
export interface GuardrailEvent {
  type: "guardrail";
  reason?: string;
}

export type AgentEvent =
  | TextEvent
  | ThinkingEvent
  | ToolCallEvent
  | ToolResultEvent
  | DoneEvent
  | ErrorEvent
  | GuardrailEvent;

/** Where the agent wants to take the user, and why. */
export interface NavigateEvent {
  path: string;
  reason?: string;
}

const str = (v: unknown): v is string => typeof v === "string";

/**
 * Parse an agent frame, checking the fields the panel actually uses.
 *
 * Returns null for anything it cannot use rather than throwing. The type check
 * alone is not enough: the agent's definition is editable and its emit list is
 * open, so a frame can arrive well-formed as JSON and wrong in its fields — and
 * each of those has a consequence. A null `text` concatenates the word "null" into
 * the answer; a `tool_call` with no id opens a chip no result can ever close; a
 * non-string `error` reaches React as an object child and takes the panel down.
 */
export function parseAgentEvent(data: string): AgentEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const frame = parsed as Record<string, unknown>;

  switch (frame.type) {
    case "text":
    case "thinking":
      if (!str(frame.text)) return null;
      return {
        type: frame.type,
        text: frame.text,
        index: typeof frame.index === "number" ? frame.index : undefined,
      };

    case "tool_call":
      if (!str(frame.tool) || !str(frame.toolCallId)) return null;
      return {
        type: "tool_call",
        tool: frame.tool,
        toolCallId: frame.toolCallId,
        input: frame.input,
      };

    case "tool_result":
      if (!str(frame.tool) || !str(frame.toolCallId)) return null;
      return {
        type: "tool_result",
        tool: frame.tool,
        toolCallId: frame.toolCallId,
        output: frame.output,
        isError: Boolean(frame.isError),
      };

    // The only one whose text is optional: it repeats what streamed, and the
    // reducer takes it only when nothing did.
    case "done":
      return { type: "done", text: str(frame.text) ? frame.text : undefined };

    case "error":
      if (!str(frame.error)) return null;
      return { type: "error", error: frame.error };

    case "guardrail":
      return { type: "guardrail", reason: str(frame.reason) ? frame.reason : undefined };

    default:
      // A kind this build has never heard of is a configuration somebody chose,
      // not a failure. Skipping it beats throwing mid-stream.
      return null;
  }
}

/**
 * Parse a navigate frame, keeping only a path this app can actually route to.
 *
 * **This is the check that matters, and it belongs here rather than in the agent.**
 * The agent is an integration the user can edit, so a guard in its definition is
 * advice; this runs on every frame regardless of what the agent was changed to say.
 * A path must be site-relative — one leading slash, and not the `//host` form that
 * a browser reads as protocol-relative and follows off-site.
 */
export function parseNavigateEvent(data: string): NavigateEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const { path, reason } = parsed as { path?: unknown; reason?: unknown };
  if (typeof path !== "string") return null;
  if (!path.startsWith("/") || path.startsWith("//")) return null;
  // A backslash is normalised to a forward slash by some browsers, so "/\evil.com"
  // can also leave the site. Nothing legitimate here contains one.
  if (path.includes("\\")) return null;
  return { path, reason: typeof reason === "string" ? reason : undefined };
}

/**
 * Pull readable text out of the route's closing `answer` frame, whose data is the
 * flow's whole result body.
 *
 * Normally this repeats what already streamed and is discarded. It earns its keep
 * on the paths where nothing streamed: the agent's guardrail answers from a
 * `set-payload`, not from the model, so that reply exists *only* here.
 */
export function parseFinalAnswer(data: string): string | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    // Not JSON at all, so it is already the text.
    return data.trim() || null;
  }
  if (typeof parsed === "string") return parsed.trim() || null;
  if (parsed && typeof parsed === "object") {
    // `answer` is what the agent's own guardrail uses; `text` and `message` are
    // what an edited one is most likely to reach for.
    for (const key of ["answer", "text", "message"]) {
      const value = (parsed as Record<string, unknown>)[key];
      if (typeof value === "string" && value.trim()) return value.trim();
    }
  }
  return null;
}

/** The default event name a frame carries when it declares none. */
const DEFAULT_EVENT = "message";

/**
 * Turn a byte stream of server-sent events into frames.
 *
 * Written as a generator over the raw reader rather than using EventSource because
 * the chat request is a POST with a body, which EventSource cannot make. It holds
 * the partial tail between chunks — a frame is split wherever TCP decides, and a
 * token stream splits often — and joins repeated `data:` lines with newlines, as
 * the format requires.
 */
export async function* parseSSE(
  stream: ReadableStream<Uint8Array>,
): AsyncGenerator<SSEFrame> {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });

      // Frames are separated by a blank line. \r\n is tolerated because the format
      // permits it and a proxy may rewrite line endings.
      let split = buffer.search(/\r?\n\r?\n/);
      while (split !== -1) {
        const raw = buffer.slice(0, split);
        buffer = buffer.slice(split + /\r?\n\r?\n/.exec(buffer.slice(split))![0].length);
        const frame = parseFrame(raw);
        if (frame) yield frame;
        split = buffer.search(/\r?\n\r?\n/);
      }
    }
    // Flush any bytes the decoder is still holding — a multi-byte character split
    // across the last chunk boundary lives there until asked for.
    buffer += decoder.decode();
    // A stream that ends without a trailing blank line still had a frame in it.
    const frame = parseFrame(buffer);
    if (frame) yield frame;
  } finally {
    reader.releaseLock();
  }
}

/** Read one frame's field lines into an event name and its data. */
function parseFrame(raw: string): SSEFrame | null {
  let event = "";
  const data: string[] = [];

  for (const line of raw.split(/\r?\n/)) {
    // A line beginning with a colon is a comment; heartbeats arrive as those.
    if (line === "" || line.startsWith(":")) continue;
    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    // Exactly one optional leading space after the colon is part of the format.
    let value = colon === -1 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);

    if (field === "event") event = value;
    else if (field === "data") data.push(value);
  }

  if (data.length === 0) return null;
  return { event: event || DEFAULT_EVENT, data: data.join("\n") };
}
