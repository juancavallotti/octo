/**
 * Reading the wire: the SSE framing, and a parser per frame that trusts none of it.
 *
 * The shapes themselves live in `frames.ts` and are re-exported here, so nothing
 * that imported this module has to know it was split.
 */

export type {
  SSEFrame,
  AgentEvent,
  NavigateEvent,
  TextEvent,
  ThinkingEvent,
  ToolCallEvent,
  ToolResultEvent,
  TurnEndEvent,
  CompactionStartEvent,
  CompactionEndEvent,
  SignalEvent,
  DoneEvent,
  ErrorEvent,
  GuardrailEvent,
} from "./frames";

import type { AgentEvent, NavigateEvent, SSEFrame } from "./frames";

const str = (v: unknown): v is string => typeof v === "string";

/** A finite number, or undefined. NaN and Infinity would render as themselves. */
const num = (v: unknown): number | undefined =>
  typeof v === "number" && Number.isFinite(v) ? v : undefined;

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
  const iteration = num(frame.iteration);

  switch (frame.type) {
    case "text":
    case "thinking":
      if (!str(frame.text)) return null;
      return {
        type: frame.type,
        iteration,
        text: frame.text,
        index: num(frame.index),
      };

    case "tool_call":
      if (!str(frame.tool) || !str(frame.toolCallId)) return null;
      return {
        type: "tool_call",
        iteration,
        tool: frame.tool,
        toolCallId: frame.toolCallId,
        input: frame.input,
      };

    case "tool_result":
      if (!str(frame.tool) || !str(frame.toolCallId)) return null;
      return {
        type: "tool_result",
        iteration,
        tool: frame.tool,
        toolCallId: frame.toolCallId,
        output: frame.output,
        isError: Boolean(frame.isError),
      };

    // Both numbers or neither: half a gauge is worse than none, because the
    // budget is per block and nothing else on the wire says whether 12,000 is
    // comfortable or one turn from being compacted.
    case "turn_end": {
      const used = num(frame.contextTokens);
      const max = num(frame.contextMaxTokens);
      if (used === undefined || max === undefined || max <= 0) return null;
      return { type: "turn_end", iteration, contextTokens: used, contextMaxTokens: max };
    }

    case "compaction_start":
      return {
        type: "compaction_start",
        iteration,
        strategy: str(frame.strategy) ? frame.strategy : undefined,
      };

    case "compaction_end":
      return { type: "compaction_end", iteration, dropped: num(frame.dropped) };

    case "signal":
      if (!str(frame.signal)) return null;
      return {
        type: "signal",
        iteration,
        signal: frame.signal,
        text: str(frame.text) ? frame.text : undefined,
      };

    // The only one whose text is optional: it repeats what streamed, and the
    // reducer takes it only when nothing did.
    case "done":
      return { type: "done", iteration, text: str(frame.text) ? frame.text : undefined };

    case "error":
      if (!str(frame.error)) return null;
      return { type: "error", iteration, error: frame.error };

    case "guardrail":
      return {
        type: "guardrail",
        iteration,
        reason: str(frame.reason) ? frame.reason : undefined,
      };

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

  // Strip the characters a URL parser discards *before* testing anything, and
  // then use the stripped value. Checking the raw string would be checking a
  // different string than the browser acts on: "/\t/evil.example" starts with a
  // single slash and holds no backslash, so it passes every test below — and the
  // browser then removes the tab, leaving "//evil.example", which is
  // protocol-relative and off-site.
  const clean = path.replace(/[\t\n\r]/g, "");

  if (!clean.startsWith("/") || clean.startsWith("//")) return null;
  // A backslash is normalised to a forward slash by some browsers, so
  // "/\evil.example" can also leave the site. Nothing legitimate here has one.
  if (clean.includes("\\")) return null;
  return { path: clean, reason: typeof reason === "string" ? reason : undefined };
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
