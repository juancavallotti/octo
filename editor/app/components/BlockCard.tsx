"use client";

import { createElement } from "react";
import type { BlockNode } from "@/app/model/document";
import { getBlockSpec, resolveIcon } from "@/app/schema";
import NodeShell from "./NodeShell";

/**
 * One leaf step in the flow, drawn as a schematic node. Drag, remove, and select
 * are handled by NodeShell; this just resolves the block's icon and label.
 */
export default function BlockCard({
  block,
  flowId,
}: {
  block: BlockNode;
  flowId: string;
}) {
  const spec = getBlockSpec(block.type);
  const icon = createElement(resolveIcon(spec?.icon ?? ""), {
    size: 20,
    className: "text-zinc-600 dark:text-zinc-300",
  });

  return (
    <NodeShell
      block={block}
      flowId={flowId}
      icon={icon}
      label={spec?.label ?? block.type}
      sublabel={block.name}
    />
  );
}
