import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";

import { useAgentChat } from "./useAgentChat";

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
    expect(result.current.turns.map((t) => [t.role, t.text])).toEqual([
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
    expect(result.current.turns[1].tools).toEqual([
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
    expect(result.current.turns[1].tools[0].failed).toBe(true);
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
    expect(result.current.turns[1].text).toBe("Hello.");
  });

  it("shows the guardrail's reason as a note rather than an answer", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(frames({ type: "guardrail", reason: "Outside my remit." })),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("write me a poem"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.turns[1].note).toBe("Outside my remit.");
    expect(result.current.turns[1].text).toBe("");
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
});
