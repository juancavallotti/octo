"use client";

import { createElement } from "react";
import { getBlockSpec, resolveIcon } from "@/app/schema";
import FlowNode from "./FlowNode";

/**
 * The node that floats under the cursor during a drag (rendered in dnd-kit's
 * DragOverlay). It mirrors a canvas node so dragging from the palette or
 * reordering a step shows the same chip moving.
 */
export default function DragPreview({ blockType }: { blockType: string }) {
  const spec = getBlockSpec(blockType);
  const icon = createElement(resolveIcon(spec?.icon ?? ""), {
    size: 20,
    className: "text-zinc-600 dark:text-zinc-300",
  });

  return (
    <div className="cursor-grabbing opacity-90">
      <FlowNode icon={icon} label={spec?.label ?? blockType} />
    </div>
  );
}
