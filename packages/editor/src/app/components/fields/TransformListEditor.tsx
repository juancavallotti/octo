"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";
import CelEditor from "../../cel/CelEditor";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/** One additive edit: a body edit ({setBody}) or a variable edit ({setVar, value}). */
type Step = { setBody?: string; setVar?: string; value?: string };

/** A row's editable shape: `kind` drives which serialized form is emitted. */
type Row = { kind: "body" | "var"; name: string; expr: string };

function toRow(step: Step): Row {
  if (step.setVar) {
    return { kind: "var", name: step.setVar, expr: step.value ?? "" };
  }
  return { kind: "body", name: "", expr: step.setBody ?? "" };
}

function toStep(row: Row): Step {
  return row.kind === "var"
    ? { setVar: row.name, value: row.expr }
    : { setBody: row.expr };
}

/**
 * Editor for multi-transform's `transforms` (the `transform-list` field type). Holds
 * ordered rows locally so a step can be retyped freely, and emits the list of
 * serialized steps ({setBody} or {setVar, value}) on every edit. Seeded once from
 * `value`; the parent remounts it per block so stale rows never leak across
 * selections. Order is preserved — it is the apply order.
 */
export default function TransformListEditor({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: Step[]) => void;
}) {
  const [rows, setRows] = useState<Row[]>(() =>
    Array.isArray(value) ? (value as Step[]).map(toRow) : [],
  );

  function commit(next: Row[]) {
    setRows(next);
    onChange(next.map(toStep));
  }

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, i) => (
        <div
          key={i}
          className="flex flex-col gap-1.5 rounded-md border border-black/10 dark:border-white/15 p-2"
        >
          <div className="flex items-center gap-1.5">
            <select
              value={row.kind}
              onChange={(e) =>
                commit(
                  rows.map((r, j) =>
                    j === i ? { ...r, kind: e.target.value as Row["kind"] } : r,
                  ),
                )
              }
              className={INPUT}
            >
              <option value="body">Set body</option>
              <option value="var">Set variable</option>
            </select>
            {row.kind === "var" && (
              <input
                type="text"
                value={row.name}
                placeholder="variable"
                onChange={(e) =>
                  commit(
                    rows.map((r, j) =>
                      j === i ? { ...r, name: e.target.value } : r,
                    ),
                  )
                }
                className={INPUT}
              />
            )}
            <button
              type="button"
              aria-label="Remove step"
              onClick={() => commit(rows.filter((_, j) => j !== i))}
              className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
            >
              <X size={14} />
            </button>
          </div>
          <CelEditor
            value={row.expr}
            placeholder="CEL expression"
            onChange={(v) =>
              commit(rows.map((r, j) => (j === i ? { ...r, expr: v } : r)))
            }
          />
        </div>
      ))}
      <button
        type="button"
        onClick={() => commit([...rows, { kind: "body", name: "", expr: "" }])}
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add step
      </button>
    </div>
  );
}
