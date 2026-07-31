"use client";

import { useRef, useState, type ReactNode } from "react";
import { Plus, Trash2 } from "lucide-react";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

interface Row {
  /** Local only, so deleting a row does not slide the row below into its identity. */
  id: number;
  key: string;
  value: string;
}

/**
 * A `Record<string, string>` edited as rows.
 *
 * The rows are local state, not derived from the record on every render, because a map
 * cannot hold the states a user passes through while filling one in: a row whose key is
 * still empty has nowhere to live, and neither does the moment between typing a key that
 * duplicates another and fixing it. The record is rebuilt from the rows on every edit,
 * taking the FIRST of any duplicate and skipping the blanks — and saying so on the rows
 * it left out, because a key that is silently not being saved is the worst of both.
 */
export default function KeyValueField({
  label,
  hint,
  value,
  onChange,
  keyPlaceholder,
  valuePlaceholder,
  addLabel,
}: {
  label: string;
  hint?: ReactNode;
  value: Record<string, string> | undefined;
  onChange: (next: Record<string, string> | undefined) => void;
  keyPlaceholder?: string;
  valuePlaceholder?: string;
  addLabel: string;
}) {
  const nextId = useRef(0);
  const [rows, setRows] = useState<Row[]>(() =>
    Object.entries(value ?? {}).map(([key, v]) => ({ id: nextId.current++, key, value: v })),
  );

  const write = (next: Row[]) => {
    setRows(next);
    const out: Record<string, string> = {};
    for (const row of next) {
      const key = row.key.trim();
      if (!key || key in out) continue;
      out[key] = row.value;
    }
    onChange(Object.keys(out).length ? out : undefined);
  };

  const edit = (id: number, patch: Partial<Row>) =>
    write(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));

  /** Whether an earlier row already claimed this key, so this one is not being saved. */
  const shadowed = (at: number) => {
    const key = rows[at].key.trim();
    return key !== "" && rows.slice(0, at).some((row) => row.key.trim() === key);
  };

  return (
    <section className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-2">
        <h3 className="text-xs font-semibold text-zinc-600 dark:text-zinc-300">{label}</h3>
      </div>
      {hint && <p className="text-[11px] text-zinc-500 dark:text-zinc-400">{hint}</p>}

      {rows.map((row, i) => (
        <div key={row.id} className="flex flex-col gap-0.5">
          <div className="flex items-center gap-1">
            <input
              type="text"
              value={row.key}
              placeholder={keyPlaceholder}
              onChange={(e) => edit(row.id, { key: e.target.value })}
              className={`${FIELD} w-2/5 font-mono text-xs ${
                shadowed(i) ? "border-amber-400/70" : ""
              }`}
            />
            <input
              type="text"
              value={row.value}
              placeholder={valuePlaceholder}
              onChange={(e) => edit(row.id, { value: e.target.value })}
              className={`${FIELD} min-w-0 flex-1 font-mono text-xs`}
            />
            <button
              type="button"
              aria-label={`Remove ${row.key.trim() || `row ${i + 1}`}`}
              onClick={() => write(rows.filter((r) => r.id !== row.id))}
              className="shrink-0 text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>
          {shadowed(i) && (
            <span className="text-[11px] text-amber-600 dark:text-amber-500">
              An earlier row already sets this, so this one is not being saved.
            </span>
          )}
        </div>
      ))}

      <button
        type="button"
        onClick={() => write([...rows, { id: nextId.current++, key: "", value: "" }])}
        className="flex items-center gap-1 self-start text-xs text-sky-600 hover:underline dark:text-sky-400"
      >
        <Plus size={13} /> {addLabel}
      </button>
    </section>
  );
}
