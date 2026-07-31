"use client";

import { useRef, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { RecordExpect, SpyExpect } from "../../suite/types";
import RecordField from "./RecordField";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

interface Row {
  /** Local only, so deleting a crossing does not hand its drafts to the next one. */
  id: number;
  value: RecordExpect;
}

/** One spy's count and records, split out so each one owns its record identities. */
export default function SpyBody({
  value,
  seen,
  onChange,
}: {
  value: SpyExpect;
  /** The crossings the last run recorded here, positionally matching the records. */
  seen?: unknown[];
  onChange: (next: SpyExpect) => void;
}) {
  const nextId = useRef(0);
  const [rows, setRows] = useState<Row[]>(() =>
    (value.records ?? []).map((r) => ({ id: nextId.current++, value: r })),
  );

  const write = (next: Row[], count: number | undefined) => {
    setRows(next);
    const out: SpyExpect = {};
    // `0` is an assertion, so only an absent count is omitted.
    if (count !== undefined) out.count = count;
    if (next.length) out.records = next.map((row) => row.value);
    onChange(out);
  };

  return (
    <>
      <label className="flex items-center gap-2">
        <span className="shrink-0 text-xs font-medium text-zinc-500">Crossings</span>
        <input
          type="number"
          min={0}
          value={value.count ?? ""}
          placeholder="not asserted"
          onChange={(e) => {
            const raw = e.target.value;
            if (raw === "") {
              write(rows, undefined);
              return;
            }
            write(rows, Math.max(0, Number(raw)));
          }}
          className={`${FIELD} w-32`}
        />
        <span className="text-[11px] text-zinc-400 dark:text-zinc-500">
          {value.count === 0 ? "asserts the block never ran" : "exactly this many"}
        </span>
      </label>

      {rows.map((row, i) => (
        <div
          key={row.id}
          className="flex flex-col gap-1.5 rounded-md border border-black/10 p-2 dark:border-white/10"
        >
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-zinc-500">
              Crossing {i + 1}
            </span>
            <button
              type="button"
              aria-label={`Remove crossing ${i + 1}`}
              onClick={() => write(rows.filter((r) => r.id !== row.id), value.count)}
              className="text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>
          <RecordField
            value={row.value}
            seen={seen?.[i]}
            onChange={(next) =>
              write(
                rows.map((r) => (r.id === row.id ? { ...r, value: next } : r)),
                value.count,
              )
            }
          />
        </div>
      ))}

      <button
        type="button"
        onClick={() => write([...rows, { id: nextId.current++, value: {} }], value.count)}
        className="flex items-center gap-1 self-start text-xs text-sky-600 hover:underline dark:text-sky-400"
      >
        <Plus size={13} /> Assert a crossing
      </button>
    </>
  );
}
