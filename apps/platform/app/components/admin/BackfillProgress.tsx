"use client";

/**
 * How much of the store has a vector.
 *
 * It is on the page because turning embeddings on does not make search semantic
 * — it makes it become semantic, over however long the backlog takes. Without
 * this an operator who configures a provider and searches immediately finds the
 * same results as before and reasonably concludes it did not work.
 */
export function BackfillProgress({ embedded, pending }: { embedded: number; pending: number }) {
  const total = embedded + pending;
  if (total === 0) return null;
  const done = Math.round((embedded / total) * 100);
  return (
    <div className="mt-4 rounded-lg border border-black/10 p-4 dark:border-white/10">
      <h2 className="text-sm font-medium">Backfill</h2>
      <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
        {pending === 0
          ? `All ${embedded.toLocaleString()} stored items are vectorized.`
          : `${embedded.toLocaleString()} of ${total.toLocaleString()} vectorized. ` +
            "The rest are searched by text until the sweep reaches them."}
      </p>
      <div
        className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-black/10 dark:bg-white/10"
        role="progressbar"
        aria-valuenow={done}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Embedding backfill progress"
      >
        <div className="h-full bg-emerald-500" style={{ width: `${done}%` }} />
      </div>
    </div>
  );
}
