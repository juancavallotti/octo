"use client";

import { createElement } from "react";
import type { BlockNode } from "@/app/model/document";
import { slotFields } from "@/app/model/document";
import { getBlockSpec, resolveIcon } from "@/app/schema";
import type { FieldSpec } from "@/app/schema/types";
import NodeShell from "./NodeShell";
import SubFlow from "./SubFlow";

/** A human label for one entry of a composite slot. */
function slotLabel(field: FieldSpec, index: number): string {
  if (field.type === "flow-list") return `Branch ${index + 1}`;
  if (field.type === "case-list") return `Case ${index + 1}`;
  return field.label;
}

/**
 * A control-flow block (if/switch/foreach/fork/scope) drawn as a node with its
 * nested sub-flows laid out side by side beneath it. Each slot is a SubFlow whose
 * FlowView recurses, so the whole tree is editable and the parent grows to fit.
 */
export default function CompositeCard({
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
    >
      <div className="mt-2 flex flex-wrap items-start justify-center gap-3">
        {slotFields(block.type).flatMap((field) =>
          (block.slots?.[field.name] ?? []).map((sub, i) => (
            <SubFlow key={sub.id} label={slotLabel(field, i)} flow={sub} />
          )),
        )}
      </div>
    </NodeShell>
  );
}
