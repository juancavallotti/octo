"use client";

import { Scissors } from "lucide-react";

/**
 * The agent shrinking its own conversation to stay inside its budget.
 *
 * Worth a line on screen because it is the one thing an agent does to *itself*
 * rather than to the model or a tool, and because it is slow: the summarize
 * strategy is a real model call, so without this the panel shows several seconds
 * of nothing and no reason for it.
 *
 * It is also the explanation for a thing people notice later — a long conversation
 * where he has forgotten the beginning. Saying it at the moment it happens is much
 * kinder than leaving it to be discovered.
 */
export default function CompactionNotice({
  done,
  dropped,
}: {
  done: boolean;
  /** How many messages went. Absent while it is still running. */
  dropped?: number;
}) {
  return (
    <p className="flex items-center gap-1.5 text-[11px] text-zinc-500 dark:text-zinc-400">
      <Scissors size={12} className={`shrink-0 ${done ? "" : "animate-pulse text-violet-500"}`} />
      {done ? (
        <span>
          Shortened the conversation to stay in budget
          {dropped ? ` — ${dropped.toLocaleString()} earlier messages summarised` : ""}.
        </span>
      ) : (
        <span>Shortening the conversation to stay in budget…</span>
      )}
    </p>
  );
}
