"use client";

import { ChevronDown, ChevronRight, Split, TriangleAlert } from "lucide-react";
import { classifySpan, type WorkClass } from "./blockClasses";
import { formatDuration } from "./format";
import { barStyle, type WaterfallRowModel } from "./chartLayout";
import type { Interval } from "./timeSpans";

/**
 * One span: its name on the left, its bar on the right.
 *
 * The bar is a positioned div rather than SVG. Every mark here is an
 * axis-aligned rectangle with a text label, which is the one shape HTML beats
 * SVG at — labels get real text layout, truncation and tooltips for free, and
 * the rows keep ordinary DOM semantics, so a treegrid gives keyboard navigation
 * and screen-reader structure without any of it being reimplemented.
 */

/** What the bar's colour claims about where the time went. */
const CLASS_COLOR: Record<WorkClass, string> = {
  io: "bg-sky-500/70",
  cpu: "bg-emerald-500/70",
  control: "bg-zinc-400/50",
  // Deliberately its own colour rather than borrowed from one of the others: an
  // unclassified block is unknown, and a chart that quietly paints it as CPU is
  // worse than one that admits it does not know.
  unclassified: "bg-amber-500/60",
};

const CLASS_LABEL: Record<WorkClass, string> = {
  io: "waiting on something outside the process",
  cpu: "running in this process",
  control: "holding the work inside it",
  unclassified: "unclassified — nobody has said what this block waits on",
};

export default function WaterfallRow({
  row,
  view,
  selected,
  onSelect,
  onToggle,
}: {
  row: WaterfallRowModel;
  view: Interval;
  selected: boolean;
  onSelect: () => void;
  onToggle: () => void;
}) {
  const { node } = row;
  const style = barStyle(node, view);
  const workClass = classifySpan(node);
  const failed = Boolean(node.record?.error) || node.kind === "flow.failed";

  return (
    <div
      role="row"
      aria-level={node.depth + 1}
      aria-selected={selected}
      aria-expanded={row.expandable ? !row.collapsed : undefined}
      onClick={onSelect}
      className={`flex h-6 cursor-default items-center text-xs transition-colors ${
        selected ? "bg-sky-500/10" : "hover:bg-black/[0.03] dark:hover:bg-white/[0.04]"
      }`}
    >
      <div
        role="gridcell"
        style={{ paddingLeft: `${node.depth * 12 + 4}px` }}
        className="flex w-64 shrink-0 items-center gap-1 overflow-hidden pr-2"
      >
        {row.expandable ? (
          <button
            type="button"
            aria-label={row.collapsed ? "Expand" : "Collapse"}
            onClick={(e) => {
              e.stopPropagation();
              onToggle();
            }}
            className="shrink-0 rounded text-zinc-400 hover:text-zinc-700 dark:hover:text-zinc-200"
          >
            {row.collapsed ? <ChevronRight size={12} /> : <ChevronDown size={12} />}
          </button>
        ) : (
          <span className="w-3 shrink-0" />
        )}

        <span
          title={node.path || node.kind}
          className={`truncate ${failed ? "text-red-500" : ""}`}
        >
          {node.label}
        </span>

        {row.collapsed && (
          <span className="shrink-0 text-[10px] text-zinc-400">+{row.hidden}</span>
        )}
        {node.lane > 0 && (
          <Split
            size={10}
            className="shrink-0 text-zinc-400"
            aria-label="ran concurrently with a sibling"
          />
        )}
        {node.inferred && (
          <TriangleAlert
            size={10}
            className="shrink-0 text-amber-500"
            aria-label="no outcome was recorded; this span is inferred from what ran inside it"
          />
        )}
      </div>

      <div role="gridcell" className="relative h-full min-w-0 flex-1">
        {style && (
          <div
            style={{ ...style, minWidth: "2px" }}
            title={tooltip(node.label, workClass, node.durationNs, node.inputMatched)}
            className={`absolute inset-y-1 rounded-sm ${
              failed ? "bg-red-500/70" : CLASS_COLOR[workClass]
            } ${node.inferred ? "border border-dashed border-amber-500/70" : ""}`}
          />
        )}
      </div>

      <div
        role="gridcell"
        className="w-20 shrink-0 pr-2 text-right font-mono text-[11px] text-zinc-500 dark:text-zinc-400"
      >
        {node.inferred ? "—" : formatDuration(node.durationNs)}
      </div>
    </div>
  );
}

function tooltip(
  label: string,
  workClass: WorkClass,
  durationNs: number,
  inputMatched: boolean,
): string {
  const lines = [`${label} — ${formatDuration(durationNs)}`, CLASS_LABEL[workClass]];
  if (!inputMatched) {
    // The runtime is explicit that pre/post pairing is unreliable under a fork.
    // A plausible wrong payload is worse than none: nothing about it looks wrong.
    lines.push("input not matched — this address ran concurrently with itself");
  }
  return lines.join("\n");
}
