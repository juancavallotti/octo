"use client";

import { useRef, useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import type { MockSpec, MockCaseSpec } from "../../suite/types";
import MockCaseFields from "./MockCaseFields";

interface Row {
  /** Local only. The fields inside a case hold their own drafts, so a case needs an
   *  identity that deleting the one above it does not hand over. */
  id: number;
  value: MockCaseSpec;
}

/**
 * What one addressed block is stood in for with: the cases it answers, tried in order,
 * and what it does when none of them matched.
 *
 * The default is not an optional extra, and the warning where it would go says so. A mock
 * REPLACES the block, so there is no real one left to fall through to — a message
 * matching no case does not run the block, it fails it. That single misunderstanding
 * accounts for most mocks that "do not work".
 */
export default function MockSpecField({
  value,
  onChange,
}: {
  value: MockSpec;
  onChange: (next: MockSpec) => void;
}) {
  const nextId = useRef(0);
  const [rows, setRows] = useState<Row[]>(() =>
    (value.cases ?? []).map((c) => ({ id: nextId.current++, value: c })),
  );

  const write = (next: Row[], fallback: MockCaseSpec | undefined) => {
    setRows(next);
    const out: MockSpec = {};
    if (next.length) out.cases = next.map((row) => row.value);
    if (fallback) out.default = fallback;
    onChange(out);
  };

  return (
    <div className="flex flex-col gap-2">
      {rows.map((row, i) => (
        <div
          key={row.id}
          className="flex flex-col gap-1.5 rounded-lg border border-black/10 p-2 dark:border-white/10"
        >
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-zinc-500">When…</span>
            <button
              type="button"
              aria-label={`Remove case ${i + 1}`}
              onClick={() => write(rows.filter((r) => r.id !== row.id), value.default)}
              className="text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>
          <MockCaseFields
            value={row.value}
            onChange={(next) =>
              write(
                rows.map((r) => (r.id === row.id ? { ...r, value: next } : r)),
                value.default,
              )
            }
          />
        </div>
      ))}

      <button
        type="button"
        onClick={() =>
          write([...rows, { id: nextId.current++, value: { when: "" } }], value.default)
        }
        className="flex items-center gap-1 self-start text-xs text-sky-600 hover:underline dark:text-sky-400"
      >
        <Plus size={13} /> Add mock case
      </button>

      <div className="flex flex-col gap-1.5 rounded-lg border border-black/10 p-2 dark:border-white/10">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-zinc-500">Otherwise…</span>
          {value.default ? (
            <button
              type="button"
              aria-label="Remove default"
              onClick={() => write(rows, undefined)}
              className="text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          ) : (
            <button
              type="button"
              onClick={() => write(rows, {})}
              className="text-xs text-sky-600 hover:underline dark:text-sky-400"
            >
              Add
            </button>
          )}
        </div>
        {value.default ? (
          <MockCaseFields
            value={value.default}
            onChange={(next) => write(rows, next)}
            isDefault
          />
        ) : (
          <p className="text-xs text-amber-600 dark:text-amber-500">
            Without this, a message matching no case <strong>fails</strong> the block — a
            mock replaces the block, so there is no real one left to fall through to.
          </p>
        )}
      </div>
    </div>
  );
}
