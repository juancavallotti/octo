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
   * Answer a tool call the run is holding. Nothing is added to the transcript
   * here: the runtime reports the decision back on the stream.
   *
   * Resolves false when the answer never reached the run, which the caller has to
   * say out loud — an unsent answer and an unanswered call look the same, and
   * both end in a denial minutes later.
   */
  authorize: (id: string, allow: boolean) => Promise<boolean>;
  reset: () => void;
  /** Replace the conversation with a stored one, and continue it. */
  resume: (threadId: string, turns: Turn[], title?: string) => void;
}

/**
 * A conversation with the agent: the requests, and the reader loop that turns
 * their frames into a transcript.
 *
 * It owns the AbortController, which is the whole hang-up chain — aborting the
 * fetch closes the agent's stream, which ends its run. Nothing here has to tell
 * the agent to stop; it only has to not swallow the abort.
 */
export function useAgentChat(
  userKey: string,
  page: string,
  onNavigate: (event: NavigateEvent) => void,
): AgentChat {
  // Destructured rather than held whole: a fresh object every render would rebuild
  // every callback that closed over it.
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
  // controller, but they still have to be cancellable.
  const steers = useRef(new Set<AbortController>());

  // Held in a ref so the reader loop is not rebuilt when the callback identity
  // changes. Assigned in an effect rather than during render, since a render can
  // be discarded and a live stream will reach for whatever is here.
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
    () => ({ apply, applySignal, takeMessage, setFinalAnswer, noteTurn, nameThread }),
    [apply, applySignal, nameThread, noteTurn, setFinalAnswer, takeMessage],
  );

  /**
   * Cancel every steer in flight. One that lands after a stop finds no run to
   * join, so the runtime claims the conversation and starts one — answering a
   * question that was cancelled, or answering into an abandoned conversation.
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
   * starts no second one: the message is injected and this flow stops with an
   * empty body. The answer, and the `signal` frame confirming the message was
   * taken, arrive on the stream already open, which is why nothing here reads a
   * response. The user turn is appended locally rather than waited for, since the
   * run injects it at the top of its next iteration.
   */
  const steer = useCallback(
    (text: string) => {
      const turnId = randomId();
      // Pending, not sent: this response says nothing about what became of the
      // message, so it waits for the run to say it took it.
      append({ ...newTurn(turnId, "user", text), delivery: "pending" });

      const controller = new AbortController();
      steers.current.add(controller);
      void (async () => {
        try {
          const res = await fetch("/api/agent/chat", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            // The question alone: the run this joins already holds the page and
            // the route catalogue, and whatever arrives here is injected verbatim.
            body: JSON.stringify({ threadId: readThreadId(userKey), message: text }),
            signal: controller.signal,
          });
          // Nothing reads the empty body, and cancelling releases the connection
          // rather than leaving it to the browser.
          await res.body?.cancel();
          if (!res.ok) throw new Error(`the agent returned ${res.status}`);
        } catch (e) {
          // An abort is a stop or a reset, not a failure. A real failure is marked
          // on the message rather than raised over the panel: it is one message
          // that did not land, and only the message itself can say which.
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
      // A run in flight takes the message rather than a second run starting.
      // The controller ref as well as `busy`, because state is only true after
      // React commits: two sends in one tick would both read a stale `busy`, and
      // the second would replace the controller the first is holding.
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
          // Aborting is how stopping and unmounting both work, not a failure.
          if ((e as Error).name !== "AbortError") setError((e as Error).message);
        } finally {
          // Both only if this run is still the current one: an aborted reader
          // unwinds a microtask after stop(), by which time a new question may
          // already be streaming.
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
    // The reader's own settling does not cover this: by the time it unwinds, the
    // controller has been released and it no longer knows this run was current.
    settlePending();
    const controller = abort.current;
    if (!controller) return;
    controller.abort();
    abort.current = null;
    setBusy(false);

    // And say so, rather than only hanging up: a stop addressed to the conversation
    // ends the run wherever it is, including behind a proxy that has not noticed
    // the socket go and on a replica this browser never spoke to.
    void post(userKey, { stop: true });
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

