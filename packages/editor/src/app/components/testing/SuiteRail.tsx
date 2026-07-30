"use client";

import { AlertCircle, FlaskConical, Plus } from "lucide-react";

/** One row's worth of what the rail knows about a flow. */
export interface RailFlow {
  name: string;
  /** Absent when the flow has no suite yet. */
  cases?: number;
  /** How many problems the suite has, if any — a suite with one will not load. */
  issues?: number;
}

/**
 * The Testing tab's left rail: every flow in the document, and what it has by way of
 * tests.
 *
 * Listed by FLOW, not by suite, and that is the point of the tab: a flow with no tests
 * is the thing you most want to see, and a list of files would leave it invisible. So a
 * flow without a suite is a row too, with an invitation instead of a count.
 *
 * The row and its add button are siblings in a flex row rather than the second nested
 * inside the first. A button inside a button is invalid, and it makes the outer one's
 * accessible name absorb the inner one's — so the row would announce itself as
 * "orders Add tests for orders".
 */
export default function SuiteRail({
  flows,
  selected,
  onSelect,
  onAdd,
  canAdd,
}: {
  flows: RailFlow[];
  selected: string | null;
  onSelect: (flow: string) => void;
  onAdd: (flow: string) => void;
  /** False for a draft, where a suite could not be stored anywhere. */
  canAdd: boolean;
}) {
  return (
    <nav
      aria-label="Flows"
      className="w-56 shrink-0 overflow-y-auto border-r border-black/10 dark:border-white/10"
    >
      <ul className="py-1">
        {flows.map((flow) => {
          const active = flow.name === selected;
          const hasSuite = flow.cases !== undefined;
          return (
            <li
              key={flow.name}
              className={`flex items-center pr-1.5 transition-colors ${
                active
                  ? "bg-black/10 dark:bg-white/15"
                  : "hover:bg-black/5 dark:hover:bg-white/10"
              }`}
            >
              <button
                type="button"
                aria-current={active ? "true" : undefined}
                onClick={() => onSelect(flow.name)}
                className={`flex min-w-0 flex-1 items-center gap-2 px-3 py-1.5 text-left text-xs ${
                  active
                    ? "text-zinc-900 dark:text-zinc-100"
                    : "text-zinc-600 dark:text-zinc-300"
                }`}
              >
                <FlaskConical
                  size={13}
                  className={`shrink-0 ${hasSuite ? "" : "opacity-30"}`}
                />
                <span className="min-w-0 flex-1 truncate">{flow.name}</span>
              </button>
              {flow.issues ? (
                <span
                  className="flex shrink-0 items-center gap-0.5 text-[11px] text-amber-600 dark:text-amber-400"
                  title={`${flow.issues} problem${flow.issues === 1 ? "" : "s"} — dolphin will not load this suite`}
                >
                  <AlertCircle size={12} />
                  {flow.issues}
                </span>
              ) : hasSuite ? (
                <span className="shrink-0 text-[11px] tabular-nums text-zinc-400 dark:text-zinc-500">
                  {flow.cases}
                </span>
              ) : (
                canAdd && (
                  <button
                    type="button"
                    aria-label={`Add tests for ${flow.name}`}
                    title={`Add tests for ${flow.name}`}
                    onClick={() => onAdd(flow.name)}
                    className="shrink-0 rounded p-0.5 text-zinc-400 hover:bg-black/10 hover:text-zinc-700 dark:hover:bg-white/15 dark:hover:text-zinc-200"
                  >
                    <Plus size={12} />
                  </button>
                )
              )}
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
