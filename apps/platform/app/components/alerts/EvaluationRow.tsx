"use client";

import { useState } from "react";
import { Badge } from "./Badge";
import {
  STATUS_CLASS,
  describeOutcome,
  explainReason,
  formatValue,
} from "./format";
import type { Evaluation } from "@/app/model/alerts";

/**
 * One row of the execution log, expandable to its per-condition outcomes.
 *
 * The expandable-row idiom the log table already uses, and here for a sharper
 * reason: the outcomes carry the thresholds they were judged against as they
 * were at the time, so a row from three weeks ago still explains itself after
 * the watch has been retuned. A collapsed row that showed only a verdict would
 * throw that away.
 */
export function EvaluationRow({ row }: { row: Evaluation }) {
  const [open, setOpen] = useState(false);

  return (
    <li className="rounded-lg border border-black/10 dark:border-white/10">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="flex w-full items-center gap-2 px-3 py-2 text-left text-xs hover:bg-black/[0.02] dark:hover:bg-white/[0.03]"
      >
        <Badge
          label={row.status}
          className={STATUS_CLASS[row.status] ?? STATUS_CLASS.ok}
        />
        <span className="font-mono text-zinc-500 dark:text-zinc-400">
          {new Date(row.evaluatedAt).toLocaleString()}
        </span>
        <span>
          {row.matched} of {row.total}
        </span>
        {row.transitioned && (
          <span className="text-zinc-500 dark:text-zinc-400">
            {row.previousPhase} → {row.phase}
          </span>
        )}
        {row.degraded && (
          <span
            className="text-amber-600 dark:text-amber-400"
            title="At least one condition could not be answered"
          >
            partly blind
          </span>
        )}
        <span className="ml-auto text-zinc-400">{row.durationMs}ms</span>
      </button>

      {open && (
        <div className="border-t border-black/5 px-3 py-2 dark:border-white/5">
          {row.error && (
            <p className="text-xs text-red-600 dark:text-red-400">
              {row.error}
            </p>
          )}
          {row.reason && (
            <p className="text-xs text-zinc-500 dark:text-zinc-400">
              {explainReason(row.reason)}
            </p>
          )}
          <ul className="mt-1 flex flex-col gap-1">
            {row.outcomes.map((o) => (
              <li key={o.conditionId} className="text-xs">
                <span className="font-medium">{o.label}</span>
                <span className="text-zinc-500 dark:text-zinc-400">
                  {" "}
                  — {describeOutcome(o)}
                </span>
                {o.reason && o.reason !== "condition_met" && (
                  <span className="block text-zinc-500 dark:text-zinc-400">
                    {explainReason(o.reason)}
                  </span>
                )}
                {o.score !== null && o.score !== undefined && (
                  <span className="block text-zinc-400">
                    score {formatValue(o.score)}
                  </span>
                )}
              </li>
            ))}
          </ul>
          {row.windowFrom && row.windowTo && (
            <p className="mt-1 text-xs text-zinc-400">
              over {new Date(row.windowFrom).toLocaleTimeString()} to{" "}
              {new Date(row.windowTo).toLocaleTimeString()}
            </p>
          )}
        </div>
      )}
    </li>
  );
}
