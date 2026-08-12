import { describe, expect, it } from "vitest";
import { parseAgentEvent, parseNavigateEvent, parseSSE, type SSEFrame } from "./events";

/** A stream that hands out exactly the chunks given, to pin chunk-boundary handling. */
function streamOf(...chunks: string[]): ReadableStream<Uint8Array> {
  const encoder = new TextEncoder();
  return new ReadableStream({
    start(controller) {
      for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
      controller.close();
    },
  });
}

async function collect(stream: ReadableStream<Uint8Array>): Promise<SSEFrame[]> {
  const out: SSEFrame[] = [];
  for await (const frame of parseSSE(stream)) out.push(frame);
  return out;
}

describe("parseSSE", () => {
  it("reads an event name and its data", async () => {
    const frames = await collect(streamOf('event: agent\ndata: {"type":"text"}\n\n'));

    expect(frames).toEqual([{ event: "agent", data: '{"type":"text"}' }]);
  });

  // The whole reason this is not a line-by-line reader: a token stream is split
  // wherever TCP decides, which is routinely mid-frame and sometimes mid-word.
  it("joins a frame split across chunk boundaries", async () => {
    const frames = await collect(streamOf('event: age', 'nt\ndata: {"type":', '"text"}\n\n'));

    expect(frames).toEqual([{ event: "agent", data: '{"type":"text"}' }]);
  });

  it("reads several frames arriving in one chunk", async () => {
    const frames = await collect(
      streamOf("event: agent\ndata: one\n\nevent: agent\ndata: two\n\n"),
    );

    expect(frames.map((f) => f.data)).toEqual(["one", "two"]);
  });

  // Repeated data lines are one payload joined with newlines — which is how any
  // multi-line answer arrives.
  it("joins repeated data lines with newlines", async () => {
    const frames = await collect(streamOf("event: agent\ndata: first\ndata: second\n\n"));

    expect(frames[0].data).toBe("first\nsecond");
  });

  it("ignores comment lines, which is what a heartbeat is", async () => {
    const frames = await collect(streamOf(": keep-alive\n\nevent: agent\ndata: x\n\n"));

    expect(frames).toEqual([{ event: "agent", data: "x" }]);
  });

  it("tolerates CRLF line endings", async () => {
    const frames = await collect(streamOf("event: agent\r\ndata: x\r\n\r\n"));

    expect(frames).toEqual([{ event: "agent", data: "x" }]);
  });

  // A stream cut off by the agent finishing, or by a proxy, still had a frame in it.
  it("yields a trailing frame that has no blank line after it", async () => {
    const frames = await collect(streamOf("event: agent\ndata: x"));

    expect(frames).toEqual([{ event: "agent", data: "x" }]);
  });

  it("defaults the event name when a frame declares none", async () => {
    const frames = await collect(streamOf("data: x\n\n"));

    expect(frames[0].event).toBe("message");
  });

  it("preserves whitespace beyond the one optional space after the colon", async () => {
    const frames = await collect(streamOf("event: agent\ndata:   indented\n\n"));

    expect(frames[0].data).toBe("  indented");
  });
});

describe("parseAgentEvent", () => {
  it("reads each frame type the panel renders", () => {
    expect(parseAgentEvent('{"type":"text","text":"hi"}')).toEqual({ type: "text", text: "hi" });
    expect(parseAgentEvent('{"type":"tool_call","tool":"octo_api","toolCallId":"c1"}')).toEqual({
      type: "tool_call",
      tool: "octo_api",
      toolCallId: "c1",
    });
    expect(parseAgentEvent('{"type":"done","text":"bye"}')).toEqual({ type: "done", text: "bye" });
  });

  // The agent's emit list is editable, so an unknown type is a configuration
  // somebody chose rather than a fault. Skipping it beats throwing mid-stream.
  it("returns null for an unknown type or unparseable data", () => {
    expect(parseAgentEvent('{"type":"turn_start"}')).toBeNull();
    expect(parseAgentEvent("not json")).toBeNull();
    expect(parseAgentEvent('"a string"')).toBeNull();
  });

  // Most of what a reasoning model produces. Dropping it is what made the panel
  // look frozen for the five seconds before an answer began.
  it("reads a thinking frame", () => {
    expect(parseAgentEvent('{"type":"thinking","text":"weighing it up","index":0}')).toEqual({
      type: "thinking",
      text: "weighing it up",
      index: 0,
    });
  });

  // A frame can be well-formed JSON and still wrong in its fields, and each of
  // these has a consequence downstream: "null" concatenated into the answer, a
  // chip no result can close, and — for a non-string error — an object handed to
  // React as a child, which takes the panel down.
  it("refuses a frame whose fields are not what the panel will do with them", () => {
    expect(parseAgentEvent('{"type":"text","text":null}')).toBeNull();
    expect(parseAgentEvent('{"type":"text","text":42}')).toBeNull();
    expect(parseAgentEvent('{"type":"text"}')).toBeNull();
    expect(parseAgentEvent('{"type":"tool_call","tool":"octo_api"}')).toBeNull();
    expect(parseAgentEvent('{"type":"tool_call","toolCallId":"c1"}')).toBeNull();
    expect(parseAgentEvent('{"type":"tool_result","tool":"octo_api"}')).toBeNull();
    expect(parseAgentEvent('{"type":"error","error":{"message":"boom"}}')).toBeNull();
  });

  // These two carry optional text, so a frame without it is still usable — the
  // reducer has a fallback for both.
  it("keeps done and guardrail frames that carry no text", () => {
    expect(parseAgentEvent('{"type":"done"}')).toEqual({ type: "done", text: undefined });
    expect(parseAgentEvent('{"type":"guardrail"}')).toEqual({
      type: "guardrail",
      reason: undefined,
    });
  });

  it("normalises isError to a boolean rather than passing it through", () => {
    const event = parseAgentEvent('{"type":"tool_result","tool":"t","toolCallId":"c1"}');

    expect(event).toEqual({
      type: "tool_result",
      tool: "t",
      toolCallId: "c1",
      output: undefined,
      isError: false,
    });
  });
});

describe("parseNavigateEvent", () => {
  it("accepts a site-relative path", () => {
    expect(parseNavigateEvent('{"path":"/platform/admin/llm","reason":"the key lives here"}')).toEqual(
      { path: "/platform/admin/llm", reason: "the key lives here" },
    );
  });

  it("keeps a path with no reason", () => {
    expect(parseNavigateEvent('{"path":"/platform"}')).toEqual({ path: "/platform", reason: undefined });
  });

  // The agent's definition is user-editable, so this is the only guard that is
  // guaranteed to run. Every one of these would take the browser off the site.
  it("refuses anything that is not a path within this app", () => {
    for (const path of [
      "https://evil.example",
      "//evil.example",
      "/\\evil.example",
      "javascript:alert(1)",
      "platform/relative",
      "",
    ]) {
      expect(parseNavigateEvent(JSON.stringify({ path }))).toBeNull();
    }
  });

  // A URL parser strips tab, LF and CR before resolving, so the string tested has
  // to be the stripped one: "/\t/evil.example" passes a naive check and becomes
  // "//evil.example" — protocol-relative, and off the site.
  it("refuses a path that only looks site-relative until the browser strips it", () => {
    for (const path of ["/\t/evil.example", "/\n/evil.example", "/\r/evil.example", "/\t\t/evil.example"]) {
      expect(parseNavigateEvent(JSON.stringify({ path }))).toBeNull();
    }
  });

  // And a stray control character inside an otherwise fine path is removed rather
  // than passed on, so what routes is what was checked.
  it("returns the stripped path, not the raw one", () => {
    expect(parseNavigateEvent(JSON.stringify({ path: "/platform/\tadmin" }))?.path).toBe(
      "/platform/admin",
    );
  });

  it("refuses a frame with no path at all", () => {
    expect(parseNavigateEvent('{"reason":"nowhere"}')).toBeNull();
    expect(parseNavigateEvent('{"path":42}')).toBeNull();
    expect(parseNavigateEvent("not json")).toBeNull();
  });
});
