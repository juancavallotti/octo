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

/**
 * Parse an agent frame. Returns null for anything unrecognised rather than
 * throwing: the agent's emit list is editable, so a frame this build has never
 * heard of is a configuration the user chose, not a failure.
 */
export function parseAgentEvent(data: string): AgentEvent | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object") return null;
  const type = (parsed as { type?: unknown }).type;
  switch (type) {
    case "text":
    case "tool_call":
    case "tool_result":
    case "done":
    case "error":
    case "guardrail":
      return parsed as AgentEvent;
    default:
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
