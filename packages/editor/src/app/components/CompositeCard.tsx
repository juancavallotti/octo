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
 * The conventional name for the slot that *is* a block's main path — what Cache
 * memoizes, what For Each repeats. A lone slot by that name needs no label,
 * because the box around it already says what it is.
 */
const BODY_SLOT = "body";

/**
 * Whether the slot entries should carry a label.
 *
 * More than one slot always needs them, to tell then from else. A single slot
 * usually does not — except when it is not the block's body: Split and Aggregate
 * have one slot that builds the *response*, while their real path is the rest of
 * the flow below. Leaving that unlabelled reads as "this is what the block runs",
 * which is precisely wrong.
 */
function shouldShowLabels(fields: FieldSpec[], slotCount: number): boolean {
  if (slotCount > 1) return true;
  return fields.length === 1 && fields[0].name !== BODY_SLOT;
}

/**
 * A control-flow block (if/switch/foreach/fork/handle-errors) drawn as a single
 * box: the icon + title sit centred at the top and the nested sub-flows lay out
 * directly inside it, so the scope stays compact. Each slot's FlowView recurses,
 * so the whole tree is editable and the box grows to fit. Entries carry a small
 * label (then/else, Case 1…, Build response) whenever the slot is not simply the
 * block's own body.
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

  const fields = slotFields(block.type);
  const slots = fields.flatMap((field) =>
    (block.slots?.[field.name] ?? []).map((sub, i) => ({
      sub,
      label: slotLabel(field, i, sub),
      // An optional slot left empty is a valid, common choice, so say what
      // leaving it empty means rather than only inviting a drop.
      hint: field.required ? "Drop a component here" : "Optional — drop a component here",
    })),
  );
  const showLabels = shouldShowLabels(fields, slots.length);

  return (
    <NodeShell
      block={block}
      flowId={flowId}
      icon={icon}
      label={spec?.label ?? block.type}
      sublabel={block.name}
      boxed
    >
      {/* Slots stay on a single row so a many-branch block (e.g. a 7-case Switch)
          grows the flow horizontally rather than wrapping; the canvas scrolls. */}
      <div className="flex flex-nowrap items-start justify-center gap-x-4 gap-y-2">
        {slots.map(({ sub, label, hint }) => (
          <div key={sub.id} className="flex min-w-[11rem] flex-col">
            {showLabels && (
              <span className="mb-1 text-center font-mono text-[11px] text-zinc-500">
                {label}
              </span>
            )}
            <FlowView flow={sub} ariaLabel={label} emptyHint={hint} />
          </div>
        ))}
      </div>
    </NodeShell>
  );
}
