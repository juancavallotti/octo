"use client";

import { ChevronDown, ChevronRight, Split, TriangleAlert } from "lucide-react";
import { classifySpan, type WorkClass } from "./blockClasses";
import { MODEL_KINDS } from "./types";
import { formatDuration } from "./format";
import { memo } from "react";
import { barRect, type WaterfallRowModel } from "./chartLayout";
import { rowElementId } from "./useTreegridKeys";

/**
 * One span: its name on the left, its bar on the right.
 *
 * The bar is a positioned div rather than SVG. Every mark here is an
 * axis-aligned rectangle with a text label, which is the one shape HTML beats
 * SVG at — labels get real text layout, truncation and tooltips for free, and
 * the rows keep ordinary DOM semantics, so a treegrid gives keyboard navigation
 * and screen-reader structure without any of it being reimplemented.
 *
 * The name and the duration are pinned to the left edge. The track is as wide as
 * the trace is long — tens of thousands of pixels on a slow one — and a duration
 * cell at the right end of that row would be a screenful of scrolling away from
 * the name it belongs to. Everything pinned has to be opaque, which means it has
 * to repaint the row's own hover and selected tints rather than covering them.
 */

/** How much of the row is pinned: 16rem of name plus 5rem of duration. */
export const LABEL_PX = 336;

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

function WaterfallRow({
  row,
  scale,
  trackPx,
  selected,
  active,
  onSelect,
  onToggle,
}: {
  row: WaterfallRowModel;
  /** Drawn pixels per nanosecond. */
  scale: number;
  trackPx: number;
  selected: boolean;
  /** The row the keyboard is on, which is not the same as the one opened. */
  active?: boolean;
  onSelect: () => void;
  onToggle: () => void;
}) {
  const { node } = row;
  const bar = barRect(node, scale);
  const workClass = classifySpan(node);
  const failed = Boolean(node.record?.error) || node.kind === "flow.failed";
  // The pinned cell paints over the row's background, so it carries its own copy
  // of it. Hover is a group rule for the same reason.
  // A tool call is a claim about *structure*, so it is marked structurally. The
  // bar's colour already says where the time went, and overloading it would make
  // "this waited on the network" and "this was a tool" the same statement.
  const tool = row.tool;
  const model = MODEL_KINDS.has(node.kind) ? node.record?.model : null;
  const tint = selected
    ? "bg-sky-500/10"
    : "bg-white group-hover:bg-black/[0.03] dark:bg-zinc-900 dark:group-hover:bg-white/[0.04]";

  return (
    <div
      id={rowElementId(node.id)}
      role="row"
      aria-level={node.depth + 1}
      aria-selected={selected}
      aria-expanded={row.expandable ? !row.collapsed : undefined}
      onClick={onSelect}
      className={`group flex h-6 cursor-default items-center text-xs transition-colors ${
        selected ? "bg-sky-500/10" : "hover:bg-black/[0.03] dark:hover:bg-white/[0.04]"
      } ${active ? "ring-1 ring-inset ring-sky-500/40" : ""}`}
    >
      <div
        role="gridcell"
        style={{ paddingLeft: `${node.depth * 12 + 4}px`, width: LABEL_PX }}
        className={`sticky left-0 z-10 flex shrink-0 items-center gap-1 overflow-hidden pr-2 ${tint} ${
          tool ? "border-l-2 border-violet-400/60" : ""
        }`}
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

        {tool && row.toolRoot && (
          <span
            title={`tool call: ${tool}`}
            className="shrink-0 rounded bg-violet-500/10 px-1 font-mono text-[10px] text-violet-600 dark:text-violet-300"
          >
            {tool}
          </span>
        )}

        {model && (
          <span
            title={`served by ${model}`}
            className="min-w-0 shrink truncate font-mono text-[10px] text-zinc-400"
          >
            {model}
          </span>
        )}

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

        <span className="ml-auto shrink-0 pl-2 font-mono text-[11px] text-zinc-500 dark:text-zinc-400">
          {node.inferred ? "—" : formatDuration(node.durationNs)}
        </span>
      </div>

      <div
        role="gridcell"
        style={{ width: trackPx }}
        className="relative h-full shrink-0"
      >
        <div
          style={bar}
          title={tooltip(node, workClass, tool)}
          className={`absolute inset-y-1 rounded-sm ${
            failed ? "bg-red-500/70" : CLASS_COLOR[workClass]
          } ${node.inferred ? "border border-dashed border-amber-500/70" : ""}`}
        />
      </div>
    </div>
  );
}

/**
 * Memoized because horizontal panning is a scroll, not a state change, and the
 * rows do not depend on it: a bar's position follows from the scale alone. Two
 * thousand rows re-rendering on every frame of a pan is the difference between a
 * chart that pans and one that stutters.
 */
export default memo(WaterfallRow);

function tooltip(
  node: WaterfallRowModel["node"],
  workClass: WorkClass,
  tool: string | null,
): string {
  // An inferred span has no measured duration — the cell shows "—" for exactly
  // that reason, and a tooltip quoting a number here would contradict it with
  // a figure derived from its children.
  const took = node.inferred
    ? "no outcome recorded; extent inferred from what ran inside it"
    : formatDuration(node.durationNs);
  const lines = [`${node.label} — ${took}`, CLASS_LABEL[workClass]];
  if (tool) lines.push(`ran inside the agent's ${tool} tool`);
  if (!node.inputMatched) {
    // The runtime is explicit that pre/post pairing is unreliable under a fork.
    // A plausible wrong payload is worse than none: nothing about it looks wrong.
    lines.push("input not matched — this address ran concurrently with itself");
  }
  return lines.join("\n");
}
