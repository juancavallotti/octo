"use client";

import { useMemo } from "react";
import { AlertCircle, Trash2 } from "lucide-react";
import { useEditorState } from "../../state/editorState";
import { blockIdAddresses } from "../../run/address";
import type { SpyExpect } from "../../suite/types";
import AddressPicker from "./AddressPicker";
import SpyBody from "./SpyBody";

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
