"use client";

import type { MemoryTranscript } from "@/app/model/agentMemory";

/**
 * One conversation, as it was had.
 *
 * Uncompacted, which is the whole reason this record exists separately from what
 * the agent still carries: working memory is pruned or summarized to fit the
 * model's window, and this is not.
 */
export function ThreadTranscript({
  transcript,
  busy,
}: {
  transcript: MemoryTranscript | null;
  busy: boolean;
}) {
  if (busy) {
    return (
      <div className="rounded-lg border border-black/10 p-4 text-sm text-zinc-500 dark:border-white/10 dark:text-zinc-400">
        Loading…
      </div>
    );
  }
  if (!transcript) {
    return (
      <div className="rounded-lg border border-black/10 p-4 text-sm text-zinc-500 dark:border-white/10 dark:text-zinc-400">
        Choose a conversation to read it.
      </div>
    );
  }
  // Labelled, because the working-memory panel beside this one deliberately shows
  // overlapping text and "which of the two is this" has to be answerable.
  return (
    <section
      aria-label="Transcript"
      className="rounded-lg border border-black/10 dark:border-white/10"
    >
      <div className="border-b border-black/10 px-4 py-3 dark:border-white/10">
        <h2 className="text-sm font-medium">
          {transcript.thread.title || transcript.thread.threadKey}
        </h2>
        <p className="mt-0.5 font-mono text-xs text-zinc-500 dark:text-zinc-400">
          {transcript.thread.threadKey}
        </p>
      </div>
      {/* No scroll container of its own: the tab panel around it scrolls, and a box
          that scrolled inside a box that scrolls is two places to be lost in. */}
      <ol className="flex flex-col gap-3 p-4">
        {transcript.turns.map((turn) => {
          // A question the run never got back to. Worth badging rather than
          // hiding: somebody asked it, and an agent that stops answering is the
          // thing an operator is looking for.
          const unanswered = turn.attrs?.unanswered === true;
          return (
            <li key={turn.seq} className="text-sm">
              <span className="mb-0.5 flex items-center gap-2 text-xs font-medium uppercase tracking-wide text-zinc-500 dark:text-zinc-400">
                {turn.role}
                {unanswered && (
                  <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] normal-case tracking-normal text-amber-700 dark:text-amber-400">
                    never answered
                  </span>
                )}
              </span>
              <p className="whitespace-pre-wrap break-words">{turn.text}</p>
            </li>
          );
        })}
        {transcript.turns.length === 0 && (
          <li className="text-sm text-zinc-500 dark:text-zinc-400">
            Nothing was recorded in this conversation.
          </li>
        )}
      </ol>
      {transcript.next && (
        <p className="border-t border-black/10 px-4 py-2 text-xs text-zinc-500 dark:border-white/10 dark:text-zinc-400">
          Showing the first {transcript.turns.length} turns of a longer conversation.
        </p>
      )}
    </section>
  );
}
