"use client";

import {
  parseAgentEvent,
  parseFinalAnswer,
  parseNavigateEvent,
  parseSSE,
  type AgentEvent,
  type NavigateEvent,
} from "./events";
import { HANDED_OVER_NOTE } from "./turns";

/**
 * One run's stream, folded into the transcript.
 *
 * Split from the hook because it is the only part that is neither state nor
 * request: given a body and somewhere to put the frames, it reads until the run
 * ends. It is also the part with the one piece of bookkeeping worth naming — the
 * turn the run is writing changes mid-stream, and everything after the change has
 * to go to the new one.
 */

/**
 * The agent turn the run is currently writing.
 *
 * Mutable, and held by the caller, because a run does not write to one turn: a
 * message read mid-answer closes the turn in progress and opens another, and the
 * caller has to know which one to close when the stream ends — including when it
 * ends by throwing.
 */
export interface RunTarget {
  turn: string;
}

/** Where the frames go. The transcript's mutators, narrowed to what a run needs. */
export interface RunSink {
  apply: (turnId: string, event: AgentEvent) => void;
  applySignal: (turnId: string, event: Extract<AgentEvent, { type: "signal" }>) => void;
  takeMessage: (currentId: string, openedId: string, text: string) => void;
  setFinalAnswer: (turnId: string, answer: string) => void;
  noteTurn: (turnId: string, note: string) => void;
}

export async function readRun(
  body: ReadableStream<Uint8Array>,
  target: RunTarget,
  sink: RunSink,
  navigate: (event: NavigateEvent) => void,
  nextId: () => string,
): Promise<void> {
  // What this stream actually delivered. Zero is not an empty answer: it is the
  // runtime saying somebody else owns this conversation — see below.
  let applied = 0;

  for await (const frame of parseSSE(body)) {
    if (frame.event === "navigate") {
      const to = parseNavigateEvent(frame.data);
      if (to) navigate(to);
      continue;
    }
    // The route's closing frame carries the flow's result body. Usually that
    // repeats what already streamed, so it is dropped — but when the guardrail
    // answered, nothing streamed and this is the only place the reply exists.
    if (frame.event === "answer") {
      const answer = parseFinalAnswer(frame.data);
      if (answer) {
        sink.setFinalAnswer(target.turn, answer);
        applied++;
      }
      continue;
    }

    const event = parseAgentEvent(frame.data);
    if (!event) continue;
    applied++;

    // A message reached the run from outside it. `context` means it was read, and
    // is the one frame that moves the run to a new turn: what follows was said
    // because of that message, so it belongs under it rather than appended to the
    // answer it interrupted.
    if (event.type === "signal") {
      if (event.signal === "context") {
        const opened = nextId();
        sink.takeMessage(target.turn, opened, event.text ?? "");
        target.turn = opened;
      } else {
        sink.applySignal(target.turn, event);
      }
      continue;
    }

    sink.apply(target.turn, event);
  }

  // Nothing arrived at all, which is the runtime saying somebody else owns this
  // conversation. Left alone it is a blank turn with no explanation, which reads
  // as a message that vanished.
  if (applied === 0) sink.noteTurn(target.turn, HANDED_OVER_NOTE);
}
