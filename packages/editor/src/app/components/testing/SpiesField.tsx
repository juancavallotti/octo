"use client";

import { useMemo, useRef, useState } from "react";
import { AlertCircle, Plus, Trash2 } from "lucide-react";
import { useEditorState } from "../../state/editorState";
import { blockIdAddresses } from "../../run/address";
import type { RecordExpect, SpyExpect } from "../../suite/types";
import AddressPicker from "./AddressPicker";
import RecordField from "./RecordField";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

interface Row {
  /** Local only, so deleting a record does not hand its drafts to the next one. */
  id: number;
  value: RecordExpect;
}

/**
 * The blocks watched during this case, and what they should have seen.
 *
 * A spy with nothing asserted is still worth adding: watching a block is what makes its
 * crossings appear in the report at all, so an empty entry is "show me what went through
 * here" rather than a half-written assertion.
 *
 * `count` is the field to be careful with. Absent means the number of crossings is not
 * asserted; `0` asserts the block never ran, which is one of the more useful things a
 * test can say ("the retry path was not taken"). They are not the same, so the field
 * distinguishes an empty box from a typed zero and never coerces one into the other.
 *
 * Records are positional — the i-th crossing — and asserting on fewer than happened is
 * fine. Asserting on more than `count` allows is a contradiction the suite can never
 * satisfy, and the rules report it.
 */
export default function SpiesField({
  flow,
  value,
  onChange,
}: {
  flow: string;
  value: Record<string, SpyExpect> | undefined;
  onChange: (next: Record<string, SpyExpect> | undefined) => void;
}) {
  const { state } = useEditorState();
  const known = useMemo(
    () => new Set(blockIdAddresses(state.document).values()),
    [state.document],
  );

  const entries = Object.entries(value ?? {});
  const write = (next: Record<string, SpyExpect>) =>
    onChange(Object.keys(next).length ? next : undefined);

  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-semibold text-zinc-600 dark:text-zinc-300">Spies</h3>
      <p className="text-[11px] text-zinc-500 dark:text-zinc-400">
        Every message crossing a watched block is recorded and reported back, whether or
        not you assert on it.
      </p>

      {entries.map(([address, spy]) => (
        <div
          key={address}
          className="flex flex-col gap-2 rounded-lg border border-black/10 p-2 dark:border-white/10"
        >
          <div className="flex items-center gap-1.5">
            <code className="min-w-0 flex-1 truncate font-mono text-xs text-zinc-700 dark:text-zinc-300">
              {address}
            </code>
            {!known.has(address) && (
              <span
                title="No block on the canvas answers to this address — it was probably renamed or removed."
                className="flex shrink-0 items-center gap-0.5 text-[11px] text-amber-600 dark:text-amber-400"
              >
                <AlertCircle size={12} />
                unknown
              </span>
            )}
            <button
              type="button"
              aria-label={`Stop watching ${address}`}
              onClick={() =>
                write(Object.fromEntries(entries.filter(([a]) => a !== address)))
              }
              className="shrink-0 text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>

          <SpyBody
            key={address}
            value={spy}
            onChange={(next) =>
              write({ ...Object.fromEntries(entries), [address]: next })
            }
          />
        </div>
      ))}

      <AddressPicker
        flow={flow}
        taken={entries.map(([a]) => a)}
        label="Watch a block"
        onPick={(address) => write({ ...Object.fromEntries(entries), [address]: {} })}
      />
    </section>
  );
}

/** One spy's count and records, split out so each one owns its record identities. */
function SpyBody({
  value,
  onChange,
}: {
  value: SpyExpect;
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
          onChange={(e) =>
            write(rows, e.target.value === "" ? undefined : Number(e.target.value))
          }
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
