"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  parseAgentEvent,
  parseNavigateEvent,
  parseSSE,
  type AgentEvent,
  type NavigateEvent,
} from "./events";
import { ROUTE_CATALOGUE } from "./routes";

/** One tool the agent called, and how it went. */
export interface ToolRun {
  id: string;
  tool: string;
  done: boolean;
  failed: boolean;
}

/** One turn in the transcript. A user turn carries only text. */
export interface Turn {
  id: string;
  role: "user" | "agent";
  text: string;
  tools: ToolRun[];
  /** Set when the agent declined the question or the run failed. */
  note?: string;
  streaming: boolean;
}

export interface AgentChat {
  turns: Turn[];
  busy: boolean;
  error: string | null;
  send: (message: string) => void;
  stop: () => void;
  reset: () => void;
}

/** Mint a thread id, keyed per user so signing out cannot resume someone else's. */
function threadKey(userKey: string): string {
  return `octo.agent.thread.${userKey}`;
}

function readThreadId(userKey: string): string {
  const key = threadKey(userKey);
  const existing = sessionStorage.getItem(key);
  if (existing) return existing;
  const minted = crypto.randomUUID();
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

  const send = useCallback(
    (message: string) => {
      const text = message.trim();
      if (!text || busy) return;

      const controller = new AbortController();
      abort.current = controller;
      setBusy(true);
      setError(null);

      const agentTurnId = crypto.randomUUID();
      setTurns((current) => [
        ...current,
        { id: crypto.randomUUID(), role: "user", text, tools: [], streaming: false },
        { id: agentTurnId, role: "agent", text: "", tools: [], streaming: true },
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
            // The route's closing frame repeats the answer the agent already
            // streamed, so rendering it would duplicate the turn.
            if (frame.event === "answer") continue;
            const event = parseAgentEvent(frame.data);
            if (event) apply(agentTurnId, event);
          }
        } catch (e) {
          // Aborting is how stopping and closing the panel both work; it is not a
          // failure to report.
          if ((e as Error).name !== "AbortError") setError((e as Error).message);
        } finally {
          setBusy(false);
          abort.current = null;
          setTurns((current) =>
            current.map((turn) =>
              turn.id === agentTurnId ? { ...turn, streaming: false } : turn,
            ),
          );
        }
      })();
    },
    [apply, busy, page, userKey],
  );

  const stop = useCallback(() => abort.current?.abort(), []);

  /**
   * Start a fresh conversation. The thread id goes too — the agent keys its memory
   * on it, so keeping it would carry the old transcript into the new conversation.
   */
  const reset = useCallback(() => {
    abort.current?.abort();
    sessionStorage.removeItem(threadKey(userKey));
    setTurns([]);
    setError(null);
  }, [userKey]);

  return { turns, busy, error, send, stop, reset };
}

/** Fold one frame into a turn. */
function reduce(turn: Turn, event: AgentEvent): Turn {
  switch (event.type) {
    case "text":
      return { ...turn, text: turn.text + event.text };

    case "tool_call":
      return {
        ...turn,
        tools: [
          ...turn.tools,
          { id: event.toolCallId, tool: event.tool, done: false, failed: false },
        ],
      };

    case "tool_result":
      return {
        ...turn,
        tools: turn.tools.map((run) =>
          run.id === event.toolCallId
            ? { ...run, done: true, failed: Boolean(event.isError) }
            : run,
        ),
      };

    // The final answer, which on a streaming run is the text already accumulated.
    // Taken only when nothing streamed, so a non-streaming agent still shows one.
    case "done":
      return turn.text ? turn : { ...turn, text: event.text ?? "" };

    case "error":
      return { ...turn, note: event.error };

    case "guardrail":
      return { ...turn, note: event.reason ?? "That one is outside my remit." };
  }
}
