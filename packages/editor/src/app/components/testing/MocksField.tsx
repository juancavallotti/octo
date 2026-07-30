"use client";

import { useMemo } from "react";
import { AlertCircle, Trash2 } from "lucide-react";
import { useEditorState } from "../../state/editorState";
import { blockIdAddresses } from "../../run/address";
import type { MockSpec } from "../../suite/types";
import AddressPicker from "./AddressPicker";
import MockSpecField from "./MockSpecField";

/**
 * The blocks stood in for, by address.
 *
 * The same component at both levels of the file, because they are the same thing: the
 * suite's `mocks:` apply to every case, and a case's override them PER ADDRESS and
 * whole-spec. Whole-spec, not merged — that is dolphin's rule (`File.MocksFor`), and it
 * is why a case that mocks an address says everything about that block rather than
 * amending what the file said. `replacing` is how the case-level field shows which of the
 * file's mocks it is taking over.
 *
 * An address in the file that no block on the canvas answers to is still rendered, and
 * flagged. It usually means a block was renamed or deleted — but deciding that for the
 * user by dropping their mock would be the same silent edit the picker exists to avoid.
 */
export default function MocksField({
  flow,
  value,
  onChange,
  replacing,
  label,
  hint,
}: {
  flow: string;
  value: Record<string, MockSpec> | undefined;
  onChange: (next: Record<string, MockSpec> | undefined) => void;
  /** Addresses the enclosing file already mocks, which this level takes over wholesale. */
  replacing?: string[];
  label: string;
  hint?: React.ReactNode;
}) {
  const { state } = useEditorState();
  const known = useMemo(
    () => new Set(blockIdAddresses(state.document).values()),
    [state.document],
  );

  const entries = Object.entries(value ?? {});

  const write = (next: Record<string, MockSpec>) =>
    onChange(Object.keys(next).length ? next : undefined);

  const remove = (address: string) =>
    write(Object.fromEntries(entries.filter(([a]) => a !== address)));

  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-semibold text-zinc-600 dark:text-zinc-300">{label}</h3>
      {hint && <p className="text-[11px] text-zinc-500 dark:text-zinc-400">{hint}</p>}

      {entries.map(([address, spec]) => (
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
              aria-label={`Remove mock for ${address}`}
              onClick={() => remove(address)}
              className="shrink-0 text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>

          {replacing?.includes(address) && (
            <p className="text-[11px] text-amber-600 dark:text-amber-500">
              Replaces the file&apos;s mock for this block entirely — the two are not
              merged.
            </p>
          )}

          <MockSpecField
            value={spec}
            onChange={(next) => write({ ...Object.fromEntries(entries), [address]: next })}
          />
        </div>
      ))}

      <AddressPicker
        flow={flow}
        taken={entries.map(([a]) => a)}
        label="Mock a block"
        onPick={(address) =>
          write({ ...Object.fromEntries(entries), [address]: { default: {} } })
        }
      />
    </section>
  );
}
