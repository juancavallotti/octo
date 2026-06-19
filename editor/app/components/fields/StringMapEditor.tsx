"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

type Entry = [key: string, value: string];

/**
 * Editor for a string-keyed string map (the `string-map` field type). Holds
 * ordered key/value rows locally so keys can be typed freely, and emits an object
 * (rows with a non-empty key) on every edit. Seeded once from `value`; the parent
 * remounts it per block so stale rows never leak across selections.
 */
export default function StringMapEditor({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: Record<string, string>) => void;
}) {
  const [entries, setEntries] = useState<Entry[]>(() =>
    value && typeof value === "object" && !Array.isArray(value)
      ? Object.entries(value as Record<string, unknown>).map(([k, v]) => [
          k,
          String(v),
        ])
      : [],
  );

  function commit(next: Entry[]) {
    setEntries(next);
    const obj: Record<string, string> = {};
    for (const [k, v] of next) if (k) obj[k] = v;
    onChange(obj);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {entries.map(([k, v], i) => (
        <div key={i} className="flex items-center gap-1.5">
          <input
            type="text"
            value={k}
            placeholder="key"
            onChange={(e) =>
              commit(entries.map((en, j) => (j === i ? [e.target.value, en[1]] : en)))
            }
            className={INPUT}
          />
          <input
            type="text"
            value={v}
            placeholder="value"
            onChange={(e) =>
              commit(entries.map((en, j) => (j === i ? [en[0], e.target.value] : en)))
            }
            className={INPUT}
          />
          <button
            type="button"
            aria-label="Remove entry"
            onClick={() => commit(entries.filter((_, j) => j !== i))}
            className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
          >
            <X size={14} />
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={() => commit([...entries, ["", ""]])}
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add entry
      </button>
    </div>
  );
}
