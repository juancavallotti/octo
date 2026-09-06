"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { EvaluationRow } from "./EvaluationRow";
import { listEvaluations, type Evaluation } from "@/app/model/alerts";

/**
 * Every time this watch was asked, newest first — including the ticks where
 * nothing happened.
 *
 * That inclusion is the whole point. An alert that did not go off is otherwise
 * indistinguishable from one that was never evaluated, and telling those apart is
 * the question asked after every missed incident. "Only what happened" is on by
 * default because it is the usual view, but turning it off has to be possible or
 * the log cannot answer the question it exists for.
 */
export function EvaluationHistory({ watchId }: { watchId: string }) {
  const [rows, setRows] = useState<Evaluation[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [notable, setNotable] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  // Toggling the filter starts a second read while the first is in flight, and
  // the older one finishing last would put the rows it fetched under the filter
  // that is no longer selected. The same guard useAlerts uses, for the same
  // reason.
  const sequence = useRef(0);

  const read = useCallback(
    async (before?: string) => {
      const mine = ++sequence.current;
      setBusy(true);
      try {
        const page = await listEvaluations({
          watchId,
          notable,
          before,
          limit: 25,
        });
        if (sequence.current !== mine) return;
        setRows((prev) => (before ? [...prev, ...page.items] : page.items));
        setCursor(page.nextBefore);
        setError(null);
      } catch (e) {
        if (sequence.current !== mine) return;
        setError((e as Error).message);
      } finally {
        if (sequence.current === mine) setBusy(false);
      }
    },
    [watchId, notable],
  );

  useEffect(() => {
    let stopped = false;
    const load = async () => {
      if (stopped) return;
      await read();
    };
    void load();
    return () => {
      stopped = true;
    };
  }, [read]);

  return (
    <section aria-label="Evaluation history">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-medium">History</h2>
        <label className="flex items-center gap-2 text-xs text-zinc-500 dark:text-zinc-400">
          <input
            type="checkbox"
            checked={notable}
            onChange={(e) => setNotable(e.target.checked)}
          />
          Only what happened
        </label>
      </div>

      {error && (
        <p role="alert" className="mt-2 text-sm text-red-600 dark:text-red-400">
          {error}
        </p>
      )}

      {rows.length === 0 && !busy ? (
        <p className="mt-3 text-sm text-zinc-500 dark:text-zinc-400">
          {notable
            ? "Nothing has happened yet. Untick the filter to see the evaluations that found nothing."
            : "This watch has not been evaluated yet."}
        </p>
      ) : (
        <ul className="mt-3 flex flex-col gap-1.5">
          {rows.map((row) => (
            <EvaluationRow key={row.id} row={row} />
          ))}
        </ul>
      )}

      {cursor && (
        <button
          type="button"
          disabled={busy}
          onClick={() => void read(cursor)}
          className="mt-3 rounded border border-black/10 px-2 py-1 text-xs hover:bg-black/5 disabled:opacity-50 dark:border-white/10 dark:hover:bg-white/5"
        >
          Load older
        </button>
      )}
    </section>
  );
}
