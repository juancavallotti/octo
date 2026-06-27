"use client";

import { createElement } from "react";
import type { BlockNode, FlowDoc } from "../model/document";
import { slotFields } from "../model/document";
import { getBlockSpec, resolveIcon } from "../schema";
import type { FieldSpec } from "../schema/types";
import NodeShell from "./NodeShell";
import FlowView from "./FlowView";

/** A human label for one entry of a composite slot. */
function slotLabel(field: FieldSpec, index: number, sub: FlowDoc): string {
  if (field.type === "flow-list") return `Branch ${index + 1}`;
  if (field.type === "case-list") return `Case ${index + 1}`;
  // Routes/tools are identified by their model-facing name; fall back to a number.
  if (field.type === "route-list") return sub.name || `Route ${index + 1}`;
  if (field.type === "tool-list") return sub.name || `Tool ${index + 1}`;
  return field.label;
}

/**
 * A control-flow block (if/switch/foreach/fork/handle-errors) drawn as a single
 * box: the icon + title sit centred at the top and the nested sub-flows lay out
 * directly inside it, so the scope stays compact. Each slot's FlowView recurses,
 * so the whole tree is editable and the box grows to fit. When a scope has more
 * than one slot the entries carry a small label (then/else, Case 1…) to tell the
 * branches apart; a single-body scope (Cache, For Each) needs none.
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

  const slots = slotFields(block.type).flatMap((field) =>
    (block.slots?.[field.name] ?? []).map((sub, i) => ({
      sub,
      label: slotLabel(field, i, sub),
    })),
  );
  const showLabels = slots.length > 1;

  return (
    <NodeShell
      block={block}
      flowId={flowId}
      icon={icon}
      label={spec?.label ?? block.type}
      sublabel={block.name}
      boxed
    >
      <div className="flex flex-wrap items-start justify-center gap-x-4 gap-y-2">
        {slots.map(({ sub, label }) => (
          <div key={sub.id} className="flex min-w-[11rem] flex-col">
            {showLabels && (
              <span className="mb-1 text-center font-mono text-[11px] text-zinc-500">
                {label}
              </span>
            )}
            <FlowView flow={sub} ariaLabel={label} emptyHint="Drop a component here" />
          </div>
        ))}
      </div>
    </NodeShell>
  );
}
