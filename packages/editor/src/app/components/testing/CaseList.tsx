"use client";

import { AlertCircle, Plus, SkipForward, Trash2 } from "lucide-react";
import type { SuiteCase } from "../../suite/types";
import type { SuiteIssue } from "../../suite/validate";

/**
 * The cases in the open suite: the lower half of the form view's middle column.
 *
 * A case is identified by its position, not its name, because the name is the thing being
 * edited — keying on it would tear the selection out from under anyone renaming a case
 * one keystroke at a time.
 */
export default function CaseList({
  cases,
  issues,
  selected,
  onSelect,
  onAdd,
  onRemove,
}: {
  cases: SuiteCase[];
  issues: SuiteIssue[];
  selected: number | null;
  onSelect: (index: number) => void;
  onAdd: () => void;
  onRemove: (index: number) => void;
}) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <ul className="min-h-0 flex-1 overflow-y-auto py-1">
        {cases.map((c, i) => {
          const active = i === selected;
          const broken = issues.some((issue) => issue.caseIndex === i);
          return (
            <li
              key={i}
              className={`flex items-center pr-1.5 transition-colors ${
                active
                  ? "bg-black/10 dark:bg-white/15"
                  : "hover:bg-black/5 dark:hover:bg-white/10"
              }`}
            >
              <button
                type="button"
                aria-current={active ? "true" : undefined}
                onClick={() => onSelect(i)}
                className={`flex min-w-0 flex-1 items-center gap-1.5 px-3 py-1.5 text-left text-xs ${
                  active
                    ? "text-zinc-900 dark:text-zinc-100"
                    : "text-zinc-600 dark:text-zinc-300"
                }`}
              >
                {c.skip && (
                  <SkipForward
                    size={12}
                    aria-label="skipped"
                    className="shrink-0 text-zinc-400"
                  />
                )}
                {broken && (
                  <AlertCircle
                    size={12}
                    aria-label="has a problem"
                    className="shrink-0 text-amber-500"
                  />
                )}
                <span
                  className={`min-w-0 flex-1 truncate ${
                    c.name.trim() ? "" : "italic text-zinc-400"
                  }`}
                >
                  {c.name.trim() || "unnamed"}
                </span>
              </button>
              <button
                type="button"
                aria-label={`Delete case ${i + 1}`}
                title={`Delete "${c.name.trim() || "unnamed"}"`}
                onClick={() => onRemove(i)}
                className="shrink-0 rounded p-0.5 text-zinc-400 hover:bg-red-500/10 hover:text-red-600 dark:hover:text-red-400"
              >
                <Trash2 size={12} />
              </button>
            </li>
          );
        })}
      </ul>
      <button
        type="button"
        onClick={onAdd}
        className="flex shrink-0 items-center gap-1.5 border-t border-black/10 px-3 py-1.5 text-xs text-sky-600 hover:bg-black/5 dark:border-white/10 dark:text-sky-400 dark:hover:bg-white/10"
      >
        <Plus size={13} /> Add case
      </button>
    </div>
  );
}
