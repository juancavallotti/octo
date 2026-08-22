"use client";

import type { Turn } from "./turns";

/**
 * What Dr. Octo is doing, said where you can always see it.
 *
 * The old sign of life was a six-pixel caret at the end of the transcript, inside
 * the scroller — so the one thing telling you anything was happening scrolled out
 * of view the moment there was anything to scroll, which is exactly when a run is
 * long enough to wonder about. This lives outside the scroller, above the
 * composer, and stays put.
 *
 * It says what he is doing rather than that he is doing something. "Running
 * octo_api" and "Shortening the conversation" are different waits with different
 * expected lengths, and a reader who knows which one they are in does not have to
 * guess whether anything is wrong.
 */
export default function WorkingStatus({ turn }: { turn: Turn | undefined }) {
  const label = statusOf(turn);
  if (!label) return null;

  return (
    <div className="flex items-center gap-2 px-3 pt-2 text-[11px] text-zinc-500 dark:text-zinc-400">
      {/* An indeterminate sweep rather than a spinner: it reads as "work is
          ongoing" at a glance, and the width is the one thing here that does not
          need a number nobody has. */}
      <span className="h-0.5 w-8 shrink-0 overflow-hidden rounded-full bg-black/10 dark:bg-white/15">
        <span className="block h-full w-1/2 animate-[agentSweep_1.2s_ease-in-out_infinite] rounded-full bg-sky-500" />
      </span>
      <span className="truncate">{label}</span>
    </div>
  );
}

/** What the run is doing right now, from the last thing it reported. */
function statusOf(turn: Turn | undefined): string | null {
  if (!turn?.streaming) return null;

  const last = turn.segments.at(-1);
  if (!last) return "Thinking…";

  switch (last.kind) {
    case "thinking":
      return "Thinking…";
    case "tools": {
      const open = last.runs.find((run) => !run.done);
      // Every call answered and the turn still open means the agent is back at
      // the model with what they returned — which is the slow part, and worth
      // saying rather than leaving as an unchanged "Running".
      return open ? `Running ${open.tool}…` : "Reading the results…";
    }
    case "compaction":
      return last.done ? "Thinking…" : "Shortening the conversation…";
    case "signal":
      return "Taking your message into account…";
    case "text":
      return "Answering…";
  }
}
