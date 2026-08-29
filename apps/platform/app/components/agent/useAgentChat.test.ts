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

/**
 * What the runtime returns for a message it handed to the run already in flight:
 * an empty body, immediately. Nothing about the message comes back this way — the
 * acknowledgement arrives on the stream that is already open — and a steer sharing
 * the run's Response would have it cancelling the very stream it is waiting on.
 */
function handedOver(): Response {
  return { ok: true, status: 200, body: null } as unknown as Response;
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

  it("takes the name the runtime gives the conversation it is running", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(frames({ type: "text", text: "three." }, { type: "thread_title", title: "Counting integrations" })),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("how many integrations"));

    await waitFor(() => expect(result.current.title).toBe("Counting integrations"));
  });

  // The panel shows one conversation. A name for another would retitle the one on
  // screen with a name that belongs somewhere nobody is looking.
  it("ignores a name reported for a different conversation", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(frames({ type: "thread_title", title: "Someone else's", thread: "other-thread" })),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("hello"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.title).toBeNull();
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
        'event: navigate\ndata: {"path":"/platform/admin/agent","reason":"here"}\n\n' +
          frames({ type: "text", text: "Taking you there." }),
      ),
    );
    const onNavigate = vi.fn();
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", onNavigate));

    act(() => result.current.send("where is the llm key"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(onNavigate).toHaveBeenCalledWith({ path: "/platform/admin/agent", reason: "here" });
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
    fetchMock
      .mockResolvedValueOnce(sseResponse(frames({ type: "text", text: "ok" })))
      .mockResolvedValue(handedOver());
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
    const run = fetchMock.mock.calls[0][1] as RequestInit;
    const steer = fetchMock.mock.calls[1][1] as RequestInit;
    expect(steer.signal).toBeDefined();
    expect(steer.signal).not.toBe(run.signal);
  });

  /**
   * The request that carries a steered message answers nothing: the runtime hands
   * it to the run in flight and stops the flow, so the POST comes back empty and
   * immediately whether the message was folded in or thrown away. Until the run
   * says which, the bubble must not claim it was read.
   */
  it("holds a steered message as unread until he says he took it", async () => {
    fetchMock
      .mockResolvedValueOnce(
        sseResponse(
          frames(
            { type: "text", text: "ok" },
            { type: "signal", signal: "context", text: "second", iteration: 2 },
          ),
        ),
      )
      .mockResolvedValue(handedOver());
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.send("second");
    });

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(
      result.current.turns.filter((t) => t.role === "user").map((t) => t.delivery),
    ).toEqual([undefined, "taken"]);

    // And it took its place in the conversation rather than staying at the bottom:
    // the run's turn is closed above it and a fresh one opened underneath, so what
    // he does about the message renders under the message.
    expect(result.current.turns.map((t) => t.role)).toEqual(["user", "agent", "user", "agent"]);
  });

  // The run this joins already holds the page and the route catalogue from its
  // opening turn, and the runtime injects whatever arrives here into that
  // conversation verbatim. Sending them again duplicates 1.5KB of context per
  // follow-up and leaves the acknowledgement carrying a string that no bubble the
  // person typed could ever be matched to.
  it("steers with the question alone, not the whole page context", async () => {
    fetchMock
      .mockResolvedValueOnce(sseResponse(frames({ type: "text", text: "ok" })))
      .mockResolvedValue(handedOver());
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.send("second");
    });

    await waitFor(() => expect(result.current.busy).toBe(false));
    const asked = JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string);
    const steered = JSON.parse((fetchMock.mock.calls[1][1] as RequestInit).body as string);
    expect(asked.routes.length).toBeGreaterThan(0);
    expect(Object.keys(steered).sort()).toEqual(["message", "threadId"]);
  });

  /**
   * A message can be handed over without this window ever knowing a run was in
   * flight — a second tab, or this one after a reload while the old run still
   * holds the claim. The runtime stops the flow with an empty body, so the stream
   * carries nothing: no frames, no answer, and a turn that renders as a blank gap
   * where an answer should be.
   */
  it("says so when the stream carried nothing because the run was already claimed", async () => {
    fetchMock.mockResolvedValue(sseResponse(""));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("what changed?"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    const agent = result.current.turns.at(-1)!;
    expect(agent.segments).toHaveLength(0);
    expect(agent.note).toMatch(/already working on this conversation/);
  });

  // An answer that arrived is an answer, however little of it there was.
  it("says nothing about a handover when the run answered", async () => {
    fetchMock.mockResolvedValue(sseResponse(frames({ type: "text", text: "three." })));
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("how many"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.turns.at(-1)?.note).toBeUndefined();
  });

  // Nothing more is coming once the stream closes — a stop, or a failure, and
  // either way the message is gone. A spinner that never resolves is worse than
  // saying so.
  it("gives up on a steered message the run ended without taking", async () => {
    fetchMock
      .mockResolvedValueOnce(sseResponse(frames({ type: "text", text: "ok" })))
      .mockResolvedValue(handedOver());
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.send("second");
    });

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.turns.at(-1)?.delivery).toBe("missed");
  });

  // Somebody else's message — a second tab on the same conversation, or this one
  // reloaded mid-run. It really did join the conversation and really did shape
  // what follows, so it is written in rather than dropped: the answer changing
  // direction with nothing said would be the reply going strange for no reason.
  it("writes in a message this window never sent", async () => {
    fetchMock.mockResolvedValue(
      sseResponse(
        frames(
          { type: "text", text: "ok" },
          { type: "signal", signal: "context", text: "from the other tab", iteration: 2 },
        ),
      ),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("first"));

    await waitFor(() => expect(result.current.busy).toBe(false));
    expect(result.current.turns.map((t) => [t.role, answerOf(t)])).toEqual([
      ["user", "first"],
      ["agent", "ok"],
      ["user", "from the other tab"],
      ["agent", ""],
    ]);
  });

  // A steer that lands after a stop finds no run to join, so the runtime claims
  // the conversation and starts one — answering, at full price, a follow-up
  // somebody has just cancelled.
  it("cancels a steer in flight when the run is stopped", async () => {
    const signals: (AbortSignal | undefined)[] = [];
    fetchMock.mockImplementation((_url: string, init: RequestInit) => {
      signals.push(init.signal ?? undefined);
      return Promise.resolve(sseResponse(frames({ type: "text", text: "ok" })));
    });
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.send("second");
    });
    act(() => result.current.stop());

    expect(signals[1]?.aborted).toBe(true);
  });

  // The reader's own settling cannot cover a stop: by the time it unwinds, the
  // controller has been released and it no longer knows the run it is closing was
  // the current one. A spinner left turning would say a message is still coming.
  it("gives up on a steered message when the run is stopped", async () => {
    fetchMock
      .mockResolvedValueOnce(sseResponse(frames({ type: "text", text: "ok" })))
      .mockResolvedValue(handedOver());
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.send("second");
    });
    expect(result.current.turns.at(-1)?.delivery).toBe("pending");

    act(() => result.current.stop());

    expect(result.current.turns.at(-1)?.delivery).toBe("missed");
  });

  // Hanging up ends the run whose stream this connection holds. A stop addressed
  // to the conversation ends it wherever it is — through a proxy that has not
  // noticed the socket go, and on a replica this browser never spoke to.
  it("tells the agent to stop as well as hanging up", async () => {
    fetchMock.mockImplementation(() =>
      Promise.resolve(sseResponse(frames({ type: "text", text: "ok" }))),
    );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => result.current.send("research this slowly"));
    act(() => result.current.stop());

    const body = JSON.parse((fetchMock.mock.calls[1][1] as RequestInit).body as string);
    expect(body.stop).toBe(true);
    // The same conversation the run is on, or it stops somebody else's.
    expect(body.threadId).toBe(
      JSON.parse((fetchMock.mock.calls[0][1] as RequestInit).body as string).threadId,
    );
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
      // A fresh response per call: a body can only be read once, and the stop
      // notification below takes one of them.
      .mockImplementation(() =>
        Promise.resolve(sseResponse(frames({ type: "text", text: "the second answer" }))),
      );
    const { result } = renderHook(() => useAgentChat("u-1", "/platform", () => {}));

    act(() => {
      result.current.send("first");
      result.current.stop();
      result.current.send("second");
    });

    await waitFor(() =>
      expect(answerOf(result.current.turns.at(-1)!)).toBe("the second answer"),
    );
    // Three: the first question, the stop that ends it, and the second question.
    expect(fetchMock).toHaveBeenCalledTimes(3);
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
