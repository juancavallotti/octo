"use client";

import { CornerDownRight, MessageSquareOff } from "lucide-react";

/**
 * Something that reached the run from outside it, and from outside this window.
 *
 * A signal about a message this window sent is shown on that message instead — see
 * UserMessage — because a line in the middle of the reply repeats text the reader
 * can already see just above it. What is left here is the case where there is no
 * such bubble to mark: a second tab on the same conversation, or this one reloaded
 * mid-run. Then a message did arrive and change the answer, and with nothing said
 * the reply simply changes direction for no visible reason.
 */
export default function SignalNotice({ signal, text }: { signal: string; text?: string }) {
  if (signal === "unanswered") {
    return (
      <p className="flex items-start gap-1.5 text-[11px] text-amber-600 dark:text-amber-400">
        <MessageSquareOff size={12} className="mt-px shrink-0" />
        {/* The text is what makes this actionable, and empty quotes would be worse
            than none — so a frame without one says the same thing without them. */}
        <span>
          {text
            ? `He ran out of steps before answering “${text}”. Ask again.`
            : "He ran out of steps before answering a message sent mid-answer. Ask again."}
        </span>
      </p>
    );
  }
  if (signal !== "context" || !text) return null;
  return (
    <p className="flex items-start gap-1.5 text-[11px] text-zinc-500 dark:text-zinc-400">
      <CornerDownRight size={12} className="mt-px shrink-0" />
      <span>Took “{text}” into account.</span>
    </p>
  );
}
