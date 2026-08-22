"use client";

import { MessageSquareOff } from "lucide-react";

/**
 * A message the run took responsibility for and never answered.
 *
 * The other outcome no longer reaches here. A message he *read* is written into
 * the transcript where he read it — see takeIn — because what followed was said
 * because of it, so it belongs in the conversation rather than as a line in the
 * middle of the reply. Nothing followed from one he never reached, so there is no
 * position in the conversation to give it, and this is what is left: the one case
 * where something a person sent goes unanswered, said out loud.
 */
export default function SignalNotice({ signal, text }: { signal: string; text?: string }) {
  if (signal !== "unanswered") return null;
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
