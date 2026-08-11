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
import { reduce, type Turn } from "./turns";

export type { ToolRun, Turn } from "./turns";

export interface AgentChat {
  turns: Turn[];
  busy: boolean;
  error: string | null;
  send: (message: string) => void;
  stop: () => void;
  reset: () => void;
}

/**
 * A random id, without assuming a secure context.
 *
 * crypto.randomUUID exists only over HTTPS or on localhost, and a self-hosted
 * platform served over plain HTTP is an ordinary way to run this. There it is
 * undefined, and calling it threw synchronously out of send() — before the try —
 * which left busy true and a controller nothing would ever clear, wedging the
 * chat for the life of the page.
 *
 * These ids key React lists and name a conversation; the conversation is scoped
 * server-side by the authenticated user, so this is not a security boundary and
 * the fallbacks only need to not collide.
 */
function randomId(): string {
  const c = globalThis.crypto;
  if (typeof c?.randomUUID === "function") return c.randomUUID();
  if (typeof c?.getRandomValues === "function") {
    const bytes = c.getRandomValues(new Uint8Array(16));
    return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

/** Mint a thread id, keyed per user so signing out cannot resume someone else's. */
function threadKey(userKey: string): string {
  return `octo.agent.thread.${userKey}`;
}

function readThreadId(userKey: string): string {
  const key = threadKey(userKey);
  const existing = sessionStorage.getItem(key);
  if (existing) return existing;
  const minted = randomId();
  sessionStorage.setItem(key, minted);
  return minted;
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

  // Held in a ref so the reader loop is not rebuilt when the callback identity
  // changes, which for an inline arrow function is every render. Assigned in an
  // effect rather than during render: a render can be discarded, and this is the
  // callback a live stream will reach for.
  const navigate = useRef(onNavigate);
  useEffect(() => {
    navigate.current = onNavigate;
  }, [onNavigate]);

  useEffect(() => {
    return () => abort.current?.abort();
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
        turn.id === turnId && !turn.text ? { ...turn, text: answer } : turn,
      ),
    );
  }, []);

  const send = useCallback(
    (message: string) => {
      const text = message.trim();
      // The controller ref, not just `busy`: state is only true after React commits,
      // so two sends in one tick would both pass a `busy` check, and the second
      // would replace the controller the first is holding — leaving Stop unable to
      // reach the stream that is actually running. The ref is set synchronously.
      if (!text || busy || abort.current) return;

      const controller = new AbortController();
      abort.current = controller;
      setBusy(true);
      setError(null);

      const agentTurnId = randomId();
      setTurns((current) => [
        ...current,
        {
          id: randomId(),
          role: "user",
          text,
          tools: [],
          thinking: "",
          streaming: false,
        },
        { id: agentTurnId, role: "agent", text: "", tools: [], thinking: "", streaming: true },
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
    [apply, busy, page, setFinalAnswer, userKey],
  );

  /**
   * End the run in progress. The ref is released here rather than left to the
   * reader's `finally`, which runs a microtask later — long enough that a send
   * immediately after a stop would be refused by the guard above.
   */
  const stop = useCallback(() => {
    const controller = abort.current;
    if (!controller) return;
    controller.abort();
    abort.current = null;
    setBusy(false);
  }, []);

  /**
   * Start a fresh conversation. The thread id goes too — the agent keys its memory
   * on it, so keeping it would carry the old transcript into the new conversation.
   */
  const reset = useCallback(() => {
    abort.current?.abort();
    abort.current = null;
    setBusy(false);
    sessionStorage.removeItem(threadKey(userKey));
    setTurns([]);
    setError(null);
  }, [userKey]);

  return { turns, busy, error, send, stop, reset };
}

