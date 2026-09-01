"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { type NavigateEvent } from "./events";
import { readRun, type RunSink, type RunTarget } from "./readRun";
import { ROUTE_CATALOGUE } from "./routes";
import { newTurn, type Turn } from "./turns";
import { useTranscript } from "./useTranscript";
import { post } from "./instruct";
import { randomId, readThreadId, threadKey } from "./thread";

export type { Segment, ToolRun, Turn } from "./turns";

export interface AgentChat {
  turns: Turn[];
  busy: boolean;
  error: string | null;
  /**
   * What the conversation on screen is called, or null for one nothing has named
   * yet. It arrives two ways: carried in by the listing that opened a stored
   * conversation, and reported by the runtime on the run that names a new one.
   */
  title: string | null;
  /**
   * Ask, or steer. A message sent while a run is in flight is handed to that run
   * rather than starting a second one — see {@link steer}.
   */
  send: (message: string) => void;
  stop: () => void;
  /**
   * Answer a tool call the run is holding in front of the person reading this
   * panel. The run is waiting on it, so nothing is added to the transcript here —
   * the runtime reports the decision back on the stream, and that frame is what
   * settles the chip.
   */
  authorize: (id: string, allow: boolean) => void;
  reset: () => void;
  /** Replace the conversation with a stored one, and continue it. */
  resume: (threadId: string, turns: Turn[], title?: string) => void;
}

/**
 * A conversation with the agent: the requests, and the reader loop that turns
 * their frames into a transcript. The transcript itself lives in useTranscript.
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
  // Destructured rather than held whole: the hook returns a fresh object every
  // render, so a callback closing over it would be rebuilt on every render too.
  const {
    turns,
    append,
    apply,
    applySignal,
    takeMessage,
    setFinalAnswer,
    setDelivery,
    noteTurn,
    endTurn,
    settlePending,
    replace,
  } = useTranscript();
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [title, setTitle] = useState<string | null>(null);
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

  const nameThread = useCallback(
    (named: string, thread?: string) => {
      if (thread && thread !== readThreadId(userKey)) return;
      setTitle(named);
    },
    [userKey],
  );

  // The transcript's mutators, as the reader wants them. Memoized so a run holds
  // one sink for its whole life rather than a new one per render.
  const sink = useMemo<RunSink>(
    // nameThread is the runtime reporting what it called this conversation, on
    // the run that opened it — so a new conversation acquires its name mid-run
    // rather than only on the next listing. A name for some other thread is
    // dropped: it would retitle the conversation on screen with one that belongs
    // to a conversation nobody is looking at.
    () => ({ apply, applySignal, takeMessage, setFinalAnswer, noteTurn, nameThread }),
    [apply, applySignal, nameThread, noteTurn, setFinalAnswer, takeMessage],
  );

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
      const turnId = randomId();
      // Pending, not sent: the response to this request says nothing about what
      // became of the message — see Delivery — so the bubble waits for the run to
      // say it took it.
      append({ ...newTurn(turnId, "user", text), delivery: "pending" });

      const controller = new AbortController();
      steers.current.add(controller);
      void (async () => {
        try {
          const res = await fetch("/api/agent/chat", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            // The question alone. The run this joins already holds the page and
            // the route catalogue from its opening turn, and the runtime injects
            // whatever arrives here into that conversation verbatim — so sending
            // them again would add 1.5KB of duplicate context per follow-up and
            // leave the acknowledgement carrying a string no bubble can match.
            body: JSON.stringify({ threadId: readThreadId(userKey), message: text }),
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
          //
          // A real failure is marked on the message rather than raised as a
          // banner. It is one message that did not land, in a panel where others
          // did, and a notice at the top of the drawer cannot say which.
          if ((e as Error).name !== "AbortError") setDelivery(turnId, "missed");
        } finally {
          steers.current.delete(controller);
        }
      })();
    },
    [append, setDelivery, userKey],
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

      // The turn the run is writing, which is not fixed: a message read mid-answer
      // closes it and opens another. Held in an object so the reader can move it
      // and the finally below still ends the right one.
      const target: RunTarget = { turn: randomId() };
      append(newTurn(randomId(), "user", text), {
        ...newTurn(target.turn, "agent"),
        streaming: true,
      });

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

          await readRun(res.body, target, sink, (to) => navigate.current(to), randomId);
        } catch (e) {
          // Aborting is how stopping and closing the panel both work; it is not a
          // failure to report.
          if ((e as Error).name !== "AbortError") setError((e as Error).message);
        } finally {
          // Both only if this run is still the current one. An aborted reader
          // unwinds a microtask after stop(), by which time a new question may
          // already be streaming — and clearing either the controller or busy then
          // would be this run switching off the next one's lights.
          const mine = abort.current === controller;
          if (mine) {
            abort.current = null;
            setBusy(false);
          }
          // Only this run's leavings are settled. A newer run is already on the
          // stream, and anything still waiting may be waiting on that one.
          endTurn(target.turn, mine);
        }
      })();
    },
    [append, busy, endTurn, page, sink, steer, userKey],
  );

  /**
   * End the run in progress. The ref is released here rather than left to the
   * reader's `finally`, which runs a microtask later — long enough that a send
   * immediately after a stop would be refused by the guard above.
   */
  const stop = useCallback(() => {
    dropSteers();
    // The reader's own settling does not cover this: by the time it unwinds the
    // controller below has been released, so it no longer knows the run it is
    // closing was the current one. Nothing is going to read a message that was
    // waiting when the run was ended, and a spinner left turning would say the
    // opposite.
    settlePending();
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
    post(userKey, { stop: true });
  }, [dropSteers, settlePending, userKey]);

  const authorize = useCallback(
    (id: string, allow: boolean) => post(userKey, { authorize: { id, allow } }),
    [userKey],
  );

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
    replace([]);
    setTitle(null);
    setError(null);
  }, [dropSteers, replace, userKey]);

  /**
   * Pick up a stored conversation. The thread id goes to sessionStorage because
   * that is what the next message is addressed to — resuming means continuing it,
   * not reading it.
   */
  const resume = useCallback(
    (threadId: string, stored: Turn[], name?: string) => {
      dropSteers();
      abort.current?.abort();
      abort.current = null;
      setBusy(false);
      sessionStorage.setItem(threadKey(userKey), threadId);
      replace(stored);
      setTitle(name ?? null);
      setError(null);
    },
    [dropSteers, replace, userKey],
  );

  return { turns, busy, error, title, send, stop, authorize, reset, resume };
}

