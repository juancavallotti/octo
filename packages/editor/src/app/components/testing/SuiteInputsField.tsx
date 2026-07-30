"use client";

import { useRef, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { SuiteInput } from "../../suite/types";
import JsonValueField from "./JsonValueField";

const FIELD =
  "w-full rounded-md border border-black/10 dark:border-white/15 bg-transparent px-2 py-1 text-sm outline-none focus:border-black/30 dark:focus:border-white/30";

interface Row {
  /** Local only, and the reason a JSON draft never lands on the wrong input. */
  id: number;
  name: string;
  input: SuiteInput;
}

/**
 * The file's `inputs:` — messages the cases share, so "a vip order" is written once and
 * named six times.
 *
 * Rows are local state for the same reason the environment's are: a map has nowhere to
 * put a half-named entry. Each row carries a local id rather than being keyed by
 * position, because the JSON fields inside hold their own drafts — keyed by position,
 * deleting a row would slide the next one into the open draft and quietly reattach it to
 * a different input.
 */
export default function SuiteInputsField({
  value,
  onChange,
}: {
  value: Record<string, SuiteInput> | undefined;
  onChange: (next: Record<string, SuiteInput> | undefined) => void;
}) {
  const nextId = useRef(0);
  const [rows, setRows] = useState<Row[]>(() =>
    Object.entries(value ?? {}).map(([name, input]) => ({
      id: nextId.current++,
      name,
      input,
    })),
  );

  const write = (next: Row[]) => {
    setRows(next);
    const out: Record<string, SuiteInput> = {};
    for (const row of next) {
      const name = row.name.trim();
      if (!name || name in out) continue;
      out[name] = row.input;
    }
    onChange(Object.keys(out).length ? out : undefined);
  };

  const edit = (id: number, patch: Partial<Row>) =>
    write(rows.map((row) => (row.id === id ? { ...row, ...patch } : row)));

  const shadowed = (at: number) => {
    const name = rows[at].name.trim();
    return name !== "" && rows.slice(0, at).some((row) => row.name.trim() === name);
  };

  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-semibold text-zinc-600 dark:text-zinc-300">
        Shared inputs
      </h3>
      <p className="text-[11px] text-zinc-500 dark:text-zinc-400">
        Named messages any case can call the flow with, by name.
      </p>

      {rows.map((row, i) => (
        <div
          key={row.id}
          className="flex flex-col gap-2 rounded-lg border border-black/10 p-2 dark:border-white/10"
        >
          <div className="flex items-center gap-1">
            <input
              type="text"
              value={row.name}
              placeholder="a vip order"
              onChange={(e) => edit(row.id, { name: e.target.value })}
              className={`${FIELD} min-w-0 flex-1 ${
                shadowed(i) ? "border-amber-400/70" : ""
              }`}
            />
            <button
              type="button"
              aria-label={`Remove input ${row.name.trim() || i + 1}`}
              onClick={() => write(rows.filter((r) => r.id !== row.id))}
              className="shrink-0 text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>
          {shadowed(i) && (
            <span className="text-[11px] text-amber-600 dark:text-amber-500">
              An earlier input already has this name, so this one is not being saved.
            </span>
          )}

          <JsonValueField
            label="Body"
            value={row.input.data}
            placeholder='{ "orderId": 42 }'
            minHeight={48}
            onChange={(data) => edit(row.id, { input: stripped({ ...row.input, data }) })}
          />
          <JsonValueField
            label="Variables"
            hint="what a source would have set"
            value={row.input.vars}
            objectOnly
            placeholder='{ "x-api-key": "dev" }'
            minHeight={48}
            onChange={(vars) =>
              edit(row.id, {
                input: stripped({ ...row.input, vars: vars as Record<string, unknown> }),
              })
            }
          />
        </div>
      ))}

      <button
        type="button"
        onClick={() =>
          write([...rows, { id: nextId.current++, name: "", input: {} }])
        }
        className="flex items-center gap-1 self-start text-xs text-sky-600 hover:underline dark:text-sky-400"
      >
        <Plus size={13} /> Add shared input
      </button>
    </section>
  );
}

/** Drop what the fields cleared, so an emptied input serializes as an empty mapping. */
function stripped(input: SuiteInput): SuiteInput {
  const out: SuiteInput = {};
  if (input.data !== undefined) out.data = input.data;
  if (input.vars !== undefined) out.vars = input.vars;
  return out;
}
