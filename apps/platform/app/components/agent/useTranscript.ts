"use client";

import { useCallback, useState } from "react";
import type { AgentEvent } from "./frames";
import { acknowledge, answerOf, reduce, settle, type Turn } from "./turns";

/**
 * The transcript, and every way a frame changes it.
 *
 * Split from the hook that fetches because these are two different jobs: this one
 * is a reducer over an array with no network in it, and the one next door is the
 * request, the claim and the abort chain. Keeping them together meant one file
 * where a change to how a signal is displayed sat inside the code that decides
 * whether a message starts a run.
 *
 * Every mutator takes the turn it is about, because a stream outlives the render
 * that started it: by the time a frame arrives the array has moved on, and the
 * only stable handle on "the turn this run is writing" is its id.
 */
export interface Transcript {
  turns: Turn[];
  /** Add turns to the end. */
  append: (...turns: Turn[]) => void;
  /** Fold a frame into the turn the run is writing. */
  apply: (turnId: string, event: AgentEvent) => void;
  /** Fold a frame that is about a message rather than about the answer. */
  applySignal: (turnId: string, event: Extract<AgentEvent, { type: "signal" }>) => void;
  /** Take the closing frame's answer, but only when nothing streamed. */
  setFinalAnswer: (turnId: string, answer: string) => void;
  /** Say what became of a message that was handed to a run. */
  setDelivery: (turnId: string, delivery: Turn["delivery"]) => void;
  /** Say why a turn has no answer in it. */
  noteTurn: (turnId: string, note: string) => void;
  /** Close the turn a run was writing, and settle what it leaves behind. */
  endTurn: (turnId: string, settleWaiting: boolean) => void;
  /** Give up on messages still waiting to be read. */
  settlePending: () => void;
  /** Replace the whole transcript: a reset, or a stored conversation. */
  replace: (turns: Turn[]) => void;
}

export function useTranscript(): Transcript {
  const [turns, setTurns] = useState<Turn[]>([]);

  const append = useCallback((...added: Turn[]) => {
    setTurns((current) => [...current, ...added]);
  }, []);

  const apply = useCallback((turnId: string, event: AgentEvent) => {
    setTurns((current) =>
      current.map((turn) => (turn.id === turnId ? reduce(turn, event) : turn)),
    );
  }, []);

  /**
   * A signal says what the run did with a message posted to it, so it is applied
   * to that message — and only falls back to the transcript when it is about one
   * this window never sent, which is what a second tab on the same conversation
   * looks like from here.
   */
  const applySignal = useCallback(
    (turnId: string, event: Extract<AgentEvent, { type: "signal" }>) => {
      setTurns((current) => {
        const acknowledged = acknowledge(current, event.signal, event.text ?? "");
        return (
          acknowledged ?? current.map((turn) => (turn.id === turnId ? reduce(turn, event) : turn))
        );
      });
    },
    [],
  );

  const setFinalAnswer = useCallback((turnId: string, answer: string) => {
    setTurns((current) =>
      current.map((turn) =>
        turn.id === turnId && !answerOf(turn)
          ? { ...turn, segments: [...turn.segments, { kind: "text", iter: 0, text: answer }] }
          : turn,
      ),
    );
  }, []);

  const setDelivery = useCallback((turnId: string, delivery: Turn["delivery"]) => {
    setTurns((current) =>
      current.map((turn) => (turn.id === turnId ? { ...turn, delivery } : turn)),
    );
  }, []);

  const noteTurn = useCallback((turnId: string, note: string) => {
    setTurns((current) => current.map((turn) => (turn.id === turnId ? { ...turn, note } : turn)));
  }, []);

  const endTurn = useCallback((turnId: string, settleWaiting: boolean) => {
    setTurns((current) => {
      const ended = current.map((turn) =>
        turn.id === turnId ? { ...turn, streaming: false } : turn,
      );
      return settleWaiting ? settle(ended) : ended;
    });
  }, []);

  return {
    turns,
    append,
    settlePending: useCallback(() => setTurns(settle), []),
    apply,
    applySignal,
    setFinalAnswer,
    setDelivery,
    noteTurn,
    endTurn,
    replace: setTurns,
  };
}
