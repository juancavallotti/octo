"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";
import CelEditor from "../../cel/CelEditor";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** One validate rule: a boolean CEL `expr` that must hold, and the `message`
 * surfaced when it does not. */
type Rule = { expr?: string; message?: string };

/**
 * Editor for a validate block's `rules` (the `rule-list` field type). Holds
 * ordered rows locally so a rule can be retyped freely, and emits the list of
 * {expr, message} entries on every edit. Seeded once from `value`; the parent
 * remounts it per block so stale rows never leak across selections. All rules
 * must evaluate true or the block rejects the message.
 */
export default function RuleListEditor({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: Rule[]) => void;
}) {
  const [rows, setRows] = useState<Rule[]>(() =>
    Array.isArray(value) ? (value as Rule[]) : [],
  );

  function commit(next: Rule[]) {
    setRows(next);
    onChange(next);
  }

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, i) => (
        <div
          key={i}
          className="flex flex-col gap-1.5 rounded-md border border-black/10 dark:border-white/15 p-2"
        >
          <div className="flex items-center gap-1.5">
            <input
              type="text"
              value={row.message ?? ""}
              placeholder="message shown when this rule fails"
              onChange={(e) =>
                commit(
                  rows.map((r, j) =>
                    j === i ? { ...r, message: e.target.value } : r,
                  ),
                )
              }
              className={INPUT}
            />
            <button
              type="button"
              aria-label="Remove rule"
              onClick={() => commit(rows.filter((_, j) => j !== i))}
              className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
            >
              <X size={14} />
            </button>
          </div>
          <CelEditor
            value={row.expr ?? ""}
            placeholder="CEL expression (must evaluate to a bool)"
            onChange={(v) =>
              commit(rows.map((r, j) => (j === i ? { ...r, expr: v } : r)))
            }
          />
        </div>
      ))}
      <button
        type="button"
        onClick={() => commit([...rows, { expr: "", message: "" }])}
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add rule
      </button>
    </div>
  );
}
