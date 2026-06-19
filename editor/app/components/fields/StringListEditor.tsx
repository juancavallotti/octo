"use client";

import { useState } from "react";
import { Plus, X } from "lucide-react";

const INPUT =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

/**
 * Editor for a list of strings (the `string-list` field type). Locally holds the
 * rows and emits the full array on every edit. Seeded once from `value`; the
 * parent remounts it per block so stale rows never leak across selections.
 */
export default function StringListEditor({
  value,
  onChange,
}: {
  value: unknown;
  onChange: (value: string[]) => void;
}) {
  const [items, setItems] = useState<string[]>(() =>
    Array.isArray(value) ? value.map(String) : [],
  );

  function commit(next: string[]) {
    setItems(next);
    onChange(next);
  }

  return (
    <div className="flex flex-col gap-1.5">
      {items.map((item, i) => (
        <div key={i} className="flex items-center gap-1.5">
          <input
            type="text"
            value={item}
            onChange={(e) =>
              commit(items.map((it, j) => (j === i ? e.target.value : it)))
            }
            className={INPUT}
          />
          <button
            type="button"
            aria-label="Remove item"
            onClick={() => commit(items.filter((_, j) => j !== i))}
            className="shrink-0 rounded p-1 text-zinc-400 transition-colors hover:text-red-500"
          >
            <X size={14} />
          </button>
        </div>
      ))}
      <button
        type="button"
        onClick={() => commit([...items, ""])}
        className="flex items-center gap-1.5 self-start rounded-md px-2 py-1 text-xs text-zinc-500 transition-colors hover:text-zinc-700 dark:hover:text-zinc-300"
      >
        <Plus size={14} />
        Add item
      </button>
    </div>
  );
}
