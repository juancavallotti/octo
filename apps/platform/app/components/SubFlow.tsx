"use client";

import type { FlowDoc } from "@/app/model/document";
import FlowView from "./FlowView";

/**
 * One nested sub-flow of a composite (a then/else/branch/case/body slot), drawn
 * as a labelled dashed container holding its own recursive FlowView — so steps
 * drop into it at a precise index and the parent grows to fit.
 */
export default function SubFlow({
  label,
  flow,
}: {
  label: string;
  flow: FlowDoc;
}) {
  return (
    <div className="flex min-w-[12rem] flex-col rounded-2xl border-2 border-dashed border-zinc-300 bg-white/40 p-3 dark:border-zinc-700 dark:bg-white/[0.02]">
      <span className="mb-2 font-mono text-[11px] text-zinc-500">{label}</span>
      <FlowView flow={flow} ariaLabel={label} emptyHint="Drop a component here" />
    </div>
  );
}
