"use client";

import { CornerDownRight, MessageSquareOff } from "lucide-react";

/**
 * Something that reached the run from outside it.
 *
 * Two of these matter to a reader. A `context` signal is a message they sent while
 * he was working — it was folded into the conversation and shaped what came next,
 * and without a line here the answer simply changes direction for no visible
 * reason. An `unanswered` one is a message he accepted and ran out of turns before
 * reaching, which is the one case where something a person sent goes unanswered,
 * and it must not be silent.
 */
export default function SignalNotice({ signal, text }: { signal: string; text?: string }) {
  if (signal === "unanswered") {
    return (
      <p className="flex items-start gap-1.5 text-[11px] text-amber-600 dark:text-amber-400">
        <MessageSquareOff size={12} className="mt-px shrink-0" />
        <span>He ran out of steps before answering “{text}”. Ask again.</span>
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
