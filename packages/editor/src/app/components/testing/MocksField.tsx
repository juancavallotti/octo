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
 * amending what the file said. `inherited` is how the case-level field shows which of the
 * file's mocks it is taking over.
 *
 * A case can also go the other way and LIFT one of the file's mocks, so that one case runs
 * the real block. In the file that is a `null`; here it is an entry with no spec under it,
 * offered by its own picker — because "run the real block here" is a thing you reach for
 * knowing which mock you mean to lift, not a state you fall into by emptying a form.
 *
 * An address in the file that no block on the canvas answers to is still rendered, and
 * flagged. It usually means a block was renamed or deleted — but deciding that for the
 * user by dropping their mock would be the same silent edit the picker exists to avoid.
 */
export default function MocksField({
  flow,
  value,
  onChange,
  inherited,
  label,
  hint,
}: {
  flow: string;
  value: Record<string, MockSpec | null> | undefined;
  onChange: (next: Record<string, MockSpec | null> | undefined) => void;
  /**
   * Addresses the enclosing file mocks: what this level takes over wholesale, and what it
   * may lift. Absent at the file level, which has nothing above it.
   */
  inherited?: string[];
  label: string;
  hint?: React.ReactNode;
}) {
  const { state } = useEditorState();
  const known = useMemo(
    () => new Set(blockIdAddresses(state.document).values()),
    [state.document],
  );

  const entries = Object.entries(value ?? {});

  const write = (next: Record<string, MockSpec | null>) =>
    onChange(Object.keys(next).length ? next : undefined);

  const set = (address: string, spec: MockSpec | null) =>
    write({ ...Object.fromEntries(entries), [address]: spec });

  const remove = (address: string) =>
    write(Object.fromEntries(entries.filter(([a]) => a !== address)));

  // Only a mock the file declares can be lifted, and only once: un-mocking anything else
  // does nothing, which is why dolphin refuses it outright rather than letting a typo read
  // as "the real block runs here" forever.
  const named = new Set(entries.map(([address]) => address));
  const liftable = (inherited ?? []).filter((address) => !named.has(address));

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
              aria-label={
                spec === null
                  ? `Restore the file's mock for ${address}`
                  : `Remove mock for ${address}`
              }
              onClick={() => remove(address)}
              className="shrink-0 text-zinc-400 hover:text-red-500"
            >
              <Trash2 size={13} />
            </button>
          </div>

          {spec === null ? (
            <NullNote address={address} inherited={inherited} />
          ) : (
            <>
              {inherited?.includes(address) && (
                <p className="text-[11px] text-amber-600 dark:text-amber-500">
                  Replaces the file&apos;s mock for this block entirely — the two are not
                  merged.
                </p>
              )}
              <MockSpecField value={spec} onChange={(next) => set(address, next)} />
            </>
          )}
        </div>
      ))}

      <AddressPicker
        flow={flow}
        taken={entries.map(([a]) => a)}
        label="Mock a block"
        onPick={(address) => set(address, { default: {} })}
      />

      {liftable.length > 0 && (
        <label className="flex items-center gap-2">
          <span className="sr-only">Run the real block</span>
          <select
            aria-label="Run the real block"
            value=""
            onChange={(e) => e.target.value && set(e.target.value, null)}
            className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 font-mono text-xs text-sky-600 outline-none focus:border-black/30 dark:border-white/15 dark:text-sky-400 dark:focus:border-white/30"
          >
            <option value="">Run the real block (lift the file&apos;s mock)…</option>
            {liftable.map((address) => (
              <option key={address} value={address}>
                {address}
              </option>
            ))}
          </select>
        </label>
      )}
    </section>
  );
}

/**
 * What an address with no spec under it means — which depends entirely on whether there
 * is a mock above it to lift.
 *
 * Over one of the file's mocks, in a case, it is the un-mock. Anywhere else it is a mock
 * that does nothing, and dolphin refuses the whole file: at the file level there is
 * nothing above to remove, and over an address the file does not mock there is nothing to
 * remove either. The form cannot produce those two — the picker only offers what can be
 * lifted — but the YAML view can, and a file typed there is read back into this form.
 * Saying "the real block runs" over an entry that will not load is the one thing this
 * note must never do.
 */
function NullNote({ address, inherited }: { address: string; inherited?: string[] }) {
  if (inherited?.includes(address)) {
    return (
      <p className="text-[11px] text-sky-700 dark:text-sky-400">
        The real block runs in this case: the file&apos;s mock for it is lifted, and
        whatever the block actually does — the HTTP call, the write, the failure —
        happens.
      </p>
    );
  }
  return (
    <p className="text-[11px] text-rose-500">
      {inherited
        ? "Nothing to lift: the file does not mock this address, so this null removes nothing. dolphin refuses the file rather than let it read as “the real block runs here”."
        : "A null lifts an inherited mock, and the file has nothing above it. Here it is a mock with no cases and no default, which dolphin refuses."}
    </p>
  );
}
