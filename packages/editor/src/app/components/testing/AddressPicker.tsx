"use client";

import { useMemo } from "react";
import { useEditorState } from "../../state/editorState";
import { blockIdAddresses } from "../../run/address";

/**
 * Picks a block by the address the RUNNER knows it by.
 *
 * Never a text field. A mock or a spy names a block to dolphin, and the only name dolphin
 * accepts is the address — `<flow>[<chain>].<block>[<branch>]…`, in which each block is
 * selected by its name, else its type. Typed by hand it is a spelling test whose failure
 * mode is a suite that loads fine and silently watches nothing.
 *
 * A block whose label would be ambiguous has no address at all (two unnamed `log` blocks
 * in one chain both answer to "log", and the runner refuses rather than guess), so it is
 * simply absent here — naming it on the canvas is what makes it addressable.
 *
 * Addresses in the flow under test are listed first. A suite may legitimately mock a
 * block in another flow the one under test calls, so the others are offered too, just
 * further down.
 */
export default function AddressPicker({
  flow,
  taken,
  onPick,
  label,
}: {
  /** The suite's flow, whose blocks are listed first. */
  flow: string;
  /** Addresses already in use here, which are not offered again. */
  taken: string[];
  onPick: (address: string) => void;
  label: string;
}) {
  const { state } = useEditorState();

  const options = useMemo(() => {
    const all = [...blockIdAddresses(state.document).values()];
    const mine = (a: string) => a === flow || a.startsWith(`${flow}.`) || a.startsWith(`${flow}[`);
    // Only the flow-under-test grouping is imposed; sort is stable, so within a group the
    // blocks stay in document order — the order they read in on the canvas.
    return all.filter((a) => !taken.includes(a)).sort((a, b) => Number(mine(b)) - Number(mine(a)));
  }, [state.document, flow, taken]);

  if (options.length === 0) {
    return (
      <p className="text-[11px] text-zinc-500 dark:text-zinc-400">
        {blockIdAddresses(state.document).size === 0
          ? "No block on the canvas can be addressed yet. Give one a name and it becomes selectable here."
          : "Every addressable block already has one."}
      </p>
    );
  }

  return (
    <label className="flex items-center gap-2">
      <span className="sr-only">{label}</span>
      <select
        aria-label={label}
        value=""
        onChange={(e) => e.target.value && onPick(e.target.value)}
        className="min-w-0 flex-1 rounded-md border border-black/10 bg-transparent px-2 py-1 font-mono text-xs text-sky-600 outline-none focus:border-black/30 dark:border-white/15 dark:text-sky-400 dark:focus:border-white/30"
      >
        <option value="">{label}…</option>
        {options.map((address) => (
          <option key={address} value={address}>
            {address}
          </option>
        ))}
      </select>
    </label>
  );
}
