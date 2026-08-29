"use client";

import { Trash2 } from "lucide-react";
import type { MemoryThread } from "@/app/model/agentMemory";

/** How a stored timestamp is shown in a list: short, and local to the reader. */
function shortDate(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return "";
  return at.toLocaleDateString(undefined, { month: "short", day: "numeric" });
}

/**
 * An agent's conversations, most recently active first.
 *
 * The turn count is shown because it is the one number that says whether a
 * conversation is worth opening — a thread with two turns is somebody who asked
 * one question, and a thread with sixty is where an agent's behaviour is
 * actually visible.
 */
export function ThreadList({
  threads,
  selected,
  onOpen,
  onDelete,
}: {
  threads: MemoryThread[];
  selected: string | null;
  onOpen: (threadKey: string) => void;
  onDelete: (threadKey: string) => void;
}) {
  if (threads.length === 0) {
    return (
      <div className="rounded-lg border border-black/10 p-4 text-sm text-zinc-500 dark:border-white/10 dark:text-zinc-400">
        This agent has not recorded a conversation yet.
      </div>
    );
  }
  return (
    // No height cap and no scroll of its own: the column it sits in scrolls, and a
    // list that stopped at 32rem inside one that did not was a second place to be
    // stuck for an agent with more conversations than that.
    <ul className="flex flex-col rounded-lg border border-black/10 dark:border-white/10">
      {threads.map((t) => {
        const active = t.threadKey === selected;
        return (
          <li
            key={t.threadKey}
            className={`group flex items-start gap-2 border-b border-black/5 last:border-0 dark:border-white/5 ${
              active ? "bg-black/5 dark:bg-white/5" : ""
            }`}
          >
            <button
              type="button"
              onClick={() => onOpen(t.threadKey)}
              // min-w-0 is what makes the truncation below work at all: a flex
              // item defaults to min-width:auto and so refuses to shrink under
              // its own content, and `truncate` on a child of something that
              // never shrinks never triggers. Without it a thread with no title
              // — which falls back to its key — widened the row until the list
              // ran out past the edge of its own card.
              className="min-w-0 flex-1 px-3 py-2 text-left"
            >
              <span className="block truncate text-sm">{t.title || t.threadKey}</span>
              {/* The person is the part allowed to be cut. Truncating the whole
                  line would take the turn count and the date with it, and those
                  are the two things this row exists to say. */}
              <span className="mt-0.5 flex items-baseline gap-1 text-xs text-zinc-500 dark:text-zinc-400">
                {t.userId && (
                  <>
                    <span className="min-w-0 truncate">{t.userId}</span>
                    <span className="shrink-0">·</span>
                  </>
                )}
                <span className="shrink-0">
                  {t.turnCount} {t.turnCount === 1 ? "turn" : "turns"} ·{" "}
                  {shortDate(t.lastActivityAt)}
                </span>
              </span>
            </button>
            <button
              type="button"
              onClick={() => onDelete(t.threadKey)}
              aria-label={`Erase the conversation ${t.title || t.threadKey}`}
              className="mr-2 mt-2 rounded p-1 text-zinc-400 opacity-0 transition hover:text-red-500 focus:opacity-100 group-hover:opacity-100"
            >
              <Trash2 className="h-4 w-4" />
            </button>
          </li>
        );
      })}
    </ul>
  );
}
