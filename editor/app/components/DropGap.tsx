"use client";

import { useDroppable } from "@dnd-kit/core";
import { gapId } from "./dnd";
import StepArrow from "./StepArrow";

/**
 * A drop target sitting between two nodes (or after the last one). Dropping here
 * inserts at `index`. Normally it just draws the connector arrow; while a drag
 * hovers it, it swells into a highlighted insertion bar. The `empty` variant is
 * the placeholder shown when a flow has no steps yet.
 */
export default function DropGap({
  flowId,
  index,
  empty = false,
  hint,
}: {
  flowId: string;
  index: number;
  empty?: boolean;
  hint?: string;
}) {
  const { setNodeRef, isOver } = useDroppable({
    id: gapId(flowId, index),
    data: { flowId, index },
  });

  if (empty) {
    return (
      <div
        ref={setNodeRef}
        className={[
          "w-full rounded-2xl border-2 border-dashed px-3 py-4 text-center text-sm transition-colors",
          isOver
            ? "border-sky-400 bg-sky-400/5 text-sky-600"
            : "border-zinc-300 text-zinc-500 dark:border-zinc-700",
        ].join(" ")}
      >
        {hint}
      </div>
    );
  }

  return (
    <div ref={setNodeRef} className="flex w-full justify-center py-1">
      {isOver ? (
        <div className="flex flex-col items-center text-sky-500">
          <div className="h-3 w-px bg-sky-400" />
          <div className="h-1.5 w-28 rounded-full bg-sky-400" />
        </div>
      ) : (
        <StepArrow />
      )}
    </div>
  );
}
