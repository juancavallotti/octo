import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";

import { useAgentChat } from "./useAgentChat";
import { newTurn } from "./turns";
import type { ToolRun, Turn } from "./turns";

/** A fetch response whose body streams the given SSE text as one chunk. */
function sseResponse(body: string): Response {
  const encoder = new TextEncoder();
  const stream = new ReadableStream<Uint8Array>({
    start(controller) {
      controller.enqueue(encoder.encode(body));
      controller.close();
    },
  });
  return { ok: true, status: 200, body: stream } as unknown as Response;
}

/** Frames as the agent's events path writes them: one SSE event per agent event. */
function frames(...events: unknown[]): string {
  return events.map((e) => `event: agent\ndata: ${JSON.stringify(e)}\n\n`).join("");
}

const fetchMock = vi.fn();

/**
 * Readers over the segment model, so the assertions below stay about behaviour.
 * A turn is an ordered log now, and these pull the three things a test cares
 * about back out of it.
 */
function answerOf(turn: Turn): string {
  return textOf(turn, "text");
}

function thinkingOf(turn: Turn): string {
  return textOf(turn, "thinking");
}

function textOf(turn: Turn, kind: "text" | "thinking"): string {
  return turn.segments
    .filter((s) => s.kind === kind)
    .map((s) => (s as { text: string }).text)
    .join("");
}

function toolsOf(turn: Turn): ToolRun[] {
    return turn.segments.flatMap((s) => (s.kind === "tools" ? s.runs : []));
}

describe("useAgentChat", () => {
  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock);
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.clearAllMocks();
    vi.unstubAllGlobals();
  });

  it("appends the question and streams the answer into one turn", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(frames({ type: "text", text: "You have " }, { type: "text", text: "three." })),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("how many integrations"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.turns.map((t) => [t.role, answerOf(t)])).toEqual([
      ["user", "how many integrations"],
      ["agent", "You have three."],
    ]);
  });

  it("opens a chip on a tool call and closes it on the result", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        frames(
          { type: "tool_call", tool: "octo_api", toolCallId: "c1" },
          { type: "tool_result", tool: "octo_api", toolCallId: "c1", isError: false },
        ),
      ),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("list them"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(toolsOf(result.current.turns[1])).toEqual([
      { id: "c1", tool: "octo_api", done: true, failed: false },
    ]);
  });

  it("marks a failed tool result as failed", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        frames(
          { type: "tool_call", tool: "octo_api", toolCallId: "c1" },
          { type: "tool_result", tool: "octo_api", toolCallId: "c1", isError: true },
        ),
      ),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("break something"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(toolsOf(result.current.turns[1])[0].failed).toBe(true);
  });

  it("routes a navigate frame to the callback", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        'event: navigate\ndata: {"path":"/platform/admin/llm","reason":"here"}\n\n' +
          frames({ type: "text", text: "Taking you there." }),
      ),
    );
    const onNavigate = vi.fn();
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", onNavigate));

    act(() => result.current.send("where is the llm key"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(onNavigate).toHaveBeenCalledWith({ path: "/platform/admin/llm", reason: "here" });
  });

  // The agent's definition is editable, so the panel is the only guaranteed guard
  // between a model-chosen path and router.push.
  it("ignores a navigate frame that would leave the site", async () => {
    fetchMock.mockResolvedValue(
      sseResponse('event: navigate\ndata: {"path":"https://evil.example"}\n\n'),
    );
    const onNavigate = vi.fn();
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", onNavigate));

    act(() => result.current.send("go somewhere"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(onNavigate).not.toHaveBeenCalled();
  });

  // The route's closing frame repeats the streamed answer; rendering it too would
  // show every answer twice.
  it("does not duplicate the answer when the closing frame repeats it", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        frames({ type: "text", text: "Hello." }) +
          'event: answer\ndata: {"answer":"Hello."}\n\n',
      ),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("hi"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(answerOf(result.current.turns[1])).toBe("Hello.");
  });

  // The reasons come from the runtime and can grow. One this build does not know is
  // still worth saying something about, but never by showing the raw string — those
  // are written for a log.
  it("falls back to a readable note for a guardrail reason it does not know", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(frames({ type: "guardrail", reason: "some future reason" })),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("write me a poem"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.turns[1].note).toBe("He stopped short of an answer.");
    expect(result.current.turns[1].note).not.toContain("some future reason");
  });

  // `busy` only becomes true once React commits, so two sends in one tick both pass
  // a state-based guard. The second would replace the controller the first is
  // holding, and Stop would then reach a stream that had already finished while the
  // live one ran on.
  // A message typed while he is working is handed to the run in flight rather than
  // starting a rival — the runtime claims the conversation, so the second request
  // is injected into it and stops with an empty body. Both messages are the
  // person's, and both belong in the transcript.
  it("hands a second message to the run in flight instead of starting a rival", async () => {
    fetchMock.mockResolvedValue(sseResponse(frames({ type: "text", text: "ok" })));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.send("second");
    });

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(
      result.current.turns.filter((t) => t.role === "user").map((t) => answerOf(t)),
    ).toEqual(["first", "second"]);

    // And it did not take the run's controller with it. Only the first request is
    // a stream, so only the first is what Stop has to be able to reach — a steer
    // that replaced the controller would leave Stop pointing at nothing.
    const steer = fetchMock.mock.calls[1][1] as RequestInit;
    expect(steer.signal).toBeUndefined();
  });

  // Stop releases the controller synchronously, so the next question is not refused
  // by the guard above. The first call rejects the way an aborted fetch really does,
  // so the abandoned reader unwinds *while* the second stream is live — which is the
  // state in which it must not clear busy or the controller out from under it.
  it("accepts a new question immediately after a stop, and lets it finish", async () => {
    fetchMock
      .mockImplementationOnce(
        (_url: string, init: { signal: AbortSignal }) =>
          new Promise((_resolve, reject) => {
            init.signal.addEventListener("abort", () =>
              reject(Object.assign(new Error("aborted"), { name: "AbortError" })),
            );
          }),
      )
      .mockResolvedValueOnce(sseResponse(frames({ type: "text", text: "the second answer" })));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.stop();
      result.current.send("second");
    });

    await waitFor(() =>
      expect(answerOf(result.current.turns.at(-1)!)).toBe("the second answer"),
    );
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(result.current.error).toBeNull();
    // The abandoned run must not have switched off the live one's lights.
    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.turns.at(-1)?.streaming).toBe(false);
  });

  // The guardrail answers from a set-payload rather than the model, so nothing
  // streams and the reply exists only in the route's closing frame. Dropping that
  // frame unconditionally left the user with a diagnostic string and no answer.
  it("shows the guardrail's reply, which arrives only in the closing frame", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        frames({ type: "guardrail", reason: "model refused" }) +
          'event: answer\ndata: {"answer":"That one is outside my remit."}\n\n',
      ),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("write me a poem"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(answerOf(result.current.turns[1])).toBe("That one is outside my remit.");
    // And the raw reason is not what the reader is shown.
    expect(result.current.turns[1].note).toBe("He declined this one.");
  });

  // Most of what a reasoning model emits, and the thing whose absence made the
  // panel look frozen. It accumulates separately from the answer so the two can be
  // shown differently.
  it("accumulates thinking apart from the answer", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        frames(
          { type: "thinking", text: "They want " },
          { type: "thinking", text: "the integrations." },
          { type: "text", text: "You have three." },
        ),
      ),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("how many"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(thinkingOf(result.current.turns[1])).toBe("They want the integrations.");
    expect(answerOf(result.current.turns[1])).toBe("You have three.");
  });

  // The chip shows them on expand, which for an agent with write access is the
  // audit trail you get without turning tracing on.
  it("keeps a tool call's arguments and its result", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        frames(
          {
            type: "tool_call",
            tool: "octo_api",
            toolCallId: "c1",
            input: { method: "GET", path: "/integrations" },
          },
          {
            type: "tool_result",
            tool: "octo_api",
            toolCallId: "c1",
            output: { count: 3 },
          },
        ),
      ),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("list them"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(toolsOf(result.current.turns[1])[0]).toEqual({
      id: "c1",
      tool: "octo_api",
      done: true,
      failed: false,
      input: { method: "GET", path: "/integrations" },
      output: { count: 3 },
    });
  });

  it("reports the proxy's error message when the request is refused", async () => {
    fetchMock.mockResolvedValue({
      ok: false,
      status: 503,
      json: async () => ({ error: "the platform agent is not deployed" }),
    } as unknown as Response);
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("hello"));

    await waitFor(() => expect(result.current.error).toBe("the platform agent is not deployed"));
  });

  // Aborting is how both Stop and closing the panel work, so it must not read as a
  // failure.
  it("does not report an aborted stream as an error", async () => {
    fetchMock.mockRejectedValue(Object.assign(new Error("aborted"), { name: "AbortError" }));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("hello"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.error).toBeNull();
  });

  it("sends the page, the route catalogue and a per-user thread id", async () => {
    fetchMock.mockResolvedValue(sseResponse(frames({ type: "text", text: "ok" })));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform/traces", () => {}));

    act(() => result.current.send("what is this"));
    await waitFor(() => expect(result.current.busy).toBe(false));

    const body = JSON.parse(fetchMock.mock.calls[0][1].body as string);
    expect(body.page).toBe("/platform/traces");
    expect(body.message).toBe("what is this");
    expect(body.routes.length).toBeGreaterThan(0);
    expect(body.threadId).toBeTruthy();
    // The identity is injected server-side; sending one would be ignored anyway, so
    // the client does not pretend to supply it.
    expect(body.user).toBeUndefined();
  });

  // The agent keys its memory on the thread id, so keeping it would carry the old
  // transcript into what the user asked to be a fresh conversation.
  it("mints a new thread id on reset", async () => {
    fetchMock.mockResolvedValue(sseResponse(frames({ type: "text", text: "ok" })));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("first"));
    await waitFor(() => expect(result.current.busy).toBe(false));
    const first = JSON.parse(fetchMock.mock.calls[0][1].body as string).threadId;

    act(() => result.current.reset());
    expect(result.current.turns).toEqual([]);

    act(() => result.current.send("second"));
    await waitFor(() => expect(result.current.busy).toBe(false));
    const second = JSON.parse(fetchMock.mock.calls[1][1].body as string).threadId;

    expect(second).not.toBe(first);
  });

  // Coming back to a conversation means continuing it, not reading it: the next
  // message has to be addressed to the same thread, or he answers it with no idea
  // what was said before.
  it("resumes a stored conversation and addresses the next message to it", async () => {
    fetchMock.mockResolvedValue(sseResponse(frames({ type: "text", text: "still here" })));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() =>
      result.current.resume("t-old", [
        newTurn("a", "user", "what did we decide"),
        newTurn("b", "agent", "to deploy on Friday"),
      ]),
    );

    expect(result.current.turns.map((t) => answerOf(t))).toEqual([
      "what did we decide",
      "to deploy on Friday",
    ]);

    act(() => result.current.send("and the logs?"));
    await waitFor(() => expect(result.current.busy).toBe(false));

    const body = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    expect(body.threadId).toBe("t-old");
  });

});
