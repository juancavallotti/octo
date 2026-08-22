"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  parseAgentEvent,
  parseFinalAnswer,
  parseNavigateEvent,
  parseSSE,
  type AgentEvent,
  type NavigateEvent,
} from "./events";
import { ROUTE_CATALOGUE } from "./routes";
import { answerOf, newTurn, reduce, type Turn } from "./turns";
import { randomId, readThreadId, threadKey } from "./thread";

export type { Segment, ToolRun, Turn } from "./turns";

export interface AgentChat {
  turns: Turn[];
  busy: boolean;
  error: string | null;
  /**
   * Ask, or steer. A message sent while a run is in flight is handed to that run
   * rather than starting a second one — see {@link steer}.
   */
  send: (message: string) => void;
  stop: () => void;
  reset: () => void;
  /** Replace the conversation with a stored one, and continue it. */
  resume: (threadId: string, turns: Turn[]) => void;
}

/**
 * A conversation with the agent: the transcript, and the reader loop that fills it.
 *
 * It owns the AbortController, which is the whole hang-up chain — aborting the
 * fetch aborts the proxy's upstream fetch, which closes the agent's stream, which
 * ends its run. Nothing here has to tell the agent to stop; it only has to not
 * swallow the abort.
 */
export function useAgentChat(
  userKey: string,
  page: string,
  onNavigate: (event: NavigateEvent) => void,
): AgentChat {
  const [turns, setTurns] = useState<Turn[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abort = useRef<AbortController | null>(null);
  // Steers in flight. They are not the run's request and must not share its
  // controller, but they still have to be cancellable — see steer and dropSteers.
  const steers = useRef(new Set<AbortController>());

  // Held in a ref so the reader loop is not rebuilt when the callback identity
  // changes, which for an inline arrow function is every render. Assigned in an
  // effect rather than during render: a render can be discarded, and this is the
  // callback a live stream will reach for.
  const navigate = useRef(onNavigate);
  useEffect(() => {
    navigate.current = onNavigate;
  }, [onNavigate]);

  /**
   * Cancel every steer in flight.
   *
   * Not tidiness. A steer that lands *after* a stop finds no run to join, so the
   * runtime claims the conversation and starts one — answering a follow-up
   * somebody has just cancelled, at full price. The same request landing after a
   * reset would answer it into the conversation that was abandoned.
   */
  const dropSteers = useCallback(() => {
    for (const controller of steers.current) controller.abort();
    steers.current.clear();
  }, []);

  useEffect(() => {
    const inFlight = steers.current;
    return () => {
      abort.current?.abort();
      for (const controller of inFlight) controller.abort();
    };
  }, []);

  /** Apply one frame to the open agent turn. */
  const apply = useCallback((turnId: string, event: AgentEvent) => {
    setTurns((current) =>
      current.map((turn) => (turn.id === turnId ? reduce(turn, event) : turn)),
    );
  }, []);

  /** Take the closing frame's answer, but only when nothing streamed. */
  const setFinalAnswer = useCallback((turnId: string, answer: string) => {
    setTurns((current) =>
      current.map((turn) =>
        turn.id === turnId && !answerOf(turn)
          ? { ...turn, segments: [...turn.segments, { kind: "text", iter: 0, text: answer }] }
          : turn,
      ),
    );
  }, []);

  /**
   * Hand a message to the run already in flight.
   *
   * The runtime claims a conversation for the length of a run, so this request
   * does not start a second one: the message is injected into the conversation the
   * agent is having and its own flow stops with an empty body. The answer — and a
   * `signal` frame confirming the message was taken — arrive on the stream that is
   * already open, which is why nothing here reads a response.
   *
   * The user turn is appended locally rather than waited for. The run injects it at
   * the top of its next iteration, which can be seconds away, and a chat that does
   * not show what you just typed reads as one that dropped it.
   */
  const steer = useCallback(
    (text: string) => {
      setTurns((current) => [...current, newTurn(randomId(), "user", text)]);

      const controller = new AbortController();
      steers.current.add(controller);
      void (async () => {
        try {
          const res = await fetch("/api/agent/chat", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              threadId: readThreadId(userKey),
              message: text,
              page,
              routes: ROUTE_CATALOGUE,
            }),
            signal: controller.signal,
          });
          // The body is the empty one the runtime returns for a message it handed
          // over. Nothing reads it, and cancelling releases the connection rather
          // than leaving it open until the browser gets round to it.
          await res.body?.cancel();
          if (!res.ok) throw new Error(`the agent returned ${res.status}`);
        } catch (e) {
          // An abort is a stop or a reset, not a failure — and reporting one over
          // an answer that is still arriving would be a failure the reader cannot
          // act on and did not cause.
          if ((e as Error).name !== "AbortError") setError("that message did not reach him");
        } finally {
          steers.current.delete(controller);
        }
      })();
    },
    [page, userKey],
  );

  const send = useCallback(
    (message: string) => {
      const text = message.trim();
      if (!text) return;
      // A run is in flight, so this message joins it instead of starting a rival.
      //
      // The controller ref rather than `busy`: state is only true after React
      // commits, so two sends in one tick would both read a stale `busy` and the
      // second would replace the controller the first is holding — leaving Stop
      // unable to reach the stream that is actually running. The ref is set
      // synchronously.
      if (busy || abort.current) {
        steer(text);
        return;
      }

      const controller = new AbortController();
      abort.current = controller;
      setBusy(true);
      setError(null);

      const agentTurnId = randomId();
      setTurns((current) => [
        ...current,
        newTurn(randomId(), "user", text),
        { ...newTurn(agentTurnId, "agent"), streaming: true },
      ]);

      void (async () => {
        try {
          const res = await fetch("/api/agent/chat", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              threadId: readThreadId(userKey),
              message: text,
              page,
              routes: ROUTE_CATALOGUE,
            }),
            signal: controller.signal,
          });

          if (!res.ok || !res.body) {
            const detail = await res.json().catch(() => null);
            throw new Error(
              (detail as { error?: string } | null)?.error ?? `the agent returned ${res.status}`,
            );
          }

          for await (const frame of parseSSE(res.body)) {
            if (frame.event === "navigate") {
              const target = parseNavigateEvent(frame.data);
              if (target) navigate.current(target);
              continue;
            }
            // The route's closing frame carries the flow's result body. Usually
            // that repeats what already streamed, so it is dropped — but when the
            // guardrail answered, nothing streamed and this is the only place the
            // reply exists.
            if (frame.event === "answer") {
              const answer = parseFinalAnswer(frame.data);
              if (answer) setFinalAnswer(agentTurnId, answer);
              continue;
            }
            const event = parseAgentEvent(frame.data);
            if (event) apply(agentTurnId, event);
          }
        } catch (e) {
          // Aborting is how stopping and closing the panel both work; it is not a
          // failure to report.
          if ((e as Error).name !== "AbortError") setError((e as Error).message);
        } finally {
          // Both only if this run is still the current one. An aborted reader
          // unwinds a microtask after stop(), by which time a new question may
          // already be streaming — and clearing either the controller or busy then
          // would be this run switching off the next one's lights.
          if (abort.current === controller) {
            abort.current = null;
            setBusy(false);
          }
          setTurns((current) =>
            current.map((turn) =>
              turn.id === agentTurnId ? { ...turn, streaming: false } : turn,
            ),
          );
        }
      })();
    },
    [apply, busy, page, setFinalAnswer, steer, userKey],
  );

  /**
   * End the run in progress. The ref is released here rather than left to the
   * reader's `finally`, which runs a microtask later — long enough that a send
   * immediately after a stop would be refused by the guard above.
   */
  const stop = useCallback(() => {
    dropSteers();
    const controller = abort.current;
    if (!controller) return;
    controller.abort();
    abort.current = null;
    setBusy(false);

    // And say so, rather than only hanging up. Closing the connection ends the run
    // whose stream this connection holds, which is almost always the same run —
    // but a stop addressed to the conversation ends it wherever it is, including
    // through a proxy that has not noticed the socket go, and on a replica this
    // browser never spoke to.
    //
    // Deliberately silent. The run is already over as far as this panel is
    // concerned, and a failure here is not something a reader can act on.
    void fetch("/api/agent/chat", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ threadId: readThreadId(userKey), stop: true, message: "" }),
    })
      .then((res) => res.body?.cancel())
      .catch(() => {});
  }, [dropSteers, userKey]);

  /**
   * Start a fresh conversation. The thread id goes too — the agent keys its memory
   * on it, so keeping it would carry the old transcript into the new conversation.
   */
  const reset = useCallback(() => {
    dropSteers();
    abort.current?.abort();
    abort.current = null;
    setBusy(false);
    sessionStorage.removeItem(threadKey(userKey));
    setTurns([]);
    setError(null);
  }, [dropSteers, userKey]);

  /**
   * Pick up a stored conversation. The thread id goes to sessionStorage because
   * that is what the next message is addressed to — resuming means continuing it,
   * not reading it.
   */
  const resume = useCallback(
    (threadId: string, stored: Turn[]) => {
      dropSteers();
      abort.current?.abort();
      abort.current = null;
      setBusy(false);
      sessionStorage.setItem(threadKey(userKey), threadId);
      setTurns(stored);
      setError(null);
    },
    [dropSteers, userKey],
  );

  return { turns, busy, error, send, stop, reset, resume };
}

