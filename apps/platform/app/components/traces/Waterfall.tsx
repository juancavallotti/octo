"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { TriangleAlert } from "lucide-react";
import { flattenWaterfall } from "./chartLayout";
import { useChartViewport } from "./useChartViewport";
import type { Waterfall as WaterfallModel, WaterfallNode } from "./types";
import WaterfallAxis from "./WaterfallAxis";
import WaterfallRow, { LABEL_PX } from "./WaterfallRow";
import { rowElementId, useTreegridKeys } from "./useTreegridKeys";

/**
 * The chart: everything that happened on one trace, nested as it ran.
 *
 * Time is drawn at a constant density — a fixed number of pixels per second —
 * rather than stretched to whatever width was available. Stretching meant a
 * 100ms trace and a 30s trace were drawn identically: duration read as a ratio
 * between siblings and never as a quantity, and two traces could not be compared
 * by looking at them. Now a long execution is a long chart, and it scrolls.
 *
 * The one exception is a trace too short to fill the window, which fills it
 * anyway. That is a floor rather than a mode — see trackWidth — so there is no
 * threshold to cross and nothing that changes behaviour partway.
 *
 * The viewport is state and the tree is not: zooming, panning and collapsing are
 * arithmetic over a value that was built once, so none of them refetch.
 *
 * There is no virtualization, here or anywhere else in this app. A trace with
 * thousands of spans is pathological — a runaway foreach, a loop nobody meant to
 * write — and it is exactly the trace someone opens to find out why. The cap is
 * what keeps opening it from being a second incident, and the notice above the
 * rows is what keeps it from looking complete when it is not.
 */

export default function Waterfall({
  waterfall,
  selectedId,
  onSelect,
}: {
  waterfall: WaterfallModel;
  selectedId: string | null;
  onSelect: (node: WaterfallNode) => void;
}) {
  const { scroller, context, view, scale, trackPx, fitted, apply, pan, fit, revealRow } =
    useChartViewport(waterfall.spanNs);

  // A different trace is a different chart: the viewport and what was folded
  // away belong to the trace they were chosen on. That reset is done by keying
  // this component on the trace (see TraceDetail) rather than by an effect that
  // clears state after the fact — the state simply never carries over.
  const [collapsed, setCollapsed] = useState<ReadonlySet<string>>(new Set());

  const { rows, cut, collapsible } = useMemo(
    () => flattenWaterfall(waterfall.roots, collapsed),
    [waterfall.roots, collapsed],
  );

  const toggle = useCallback((id: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);
      if (!next.delete(id)) next.add(id);
      return next;
    });
  }, []);

  // Arrow keys walk and fold the tree, shifted arrows pan, Escape fits. Scoped
  // to the chart rather than to the document: Escape belongs to whatever the
  // reader is actually in, and a chart that claims it globally takes it from
  // every dialog on the page.
  const keys = useTreegridKeys(
    rows,
    (row) => onSelect(row.node),
    (row) => toggle(row.node.id),
    pan,
    fit,
  );

  // Keep the row the keyboard is on in view. The scroller belongs to the
  // viewport, so the scrolling does too — see revealRow for why it must not move
  // the chart along the trace while it does.
  useEffect(() => {
    if (keys.activeId) revealRow(keys.activeId);
  }, [keys.activeId, revealRow]);

  return (
    <div className="flex h-full min-h-0 flex-col">
      {waterfall.warnings.length > 0 && (
        <ul className="space-y-1 px-3 py-2">
          {waterfall.warnings.map((warning) => (
            <li
              key={warning.kind}
              className="flex items-start gap-1.5 text-xs text-amber-600 dark:text-amber-400"
            >
              <TriangleAlert size={12} className="mt-0.5 shrink-0" />
              {warning.message}
            </li>
          ))}
        </ul>
      )}

      <div className="flex items-center gap-2 px-3 pb-1 text-xs text-zinc-500 dark:text-zinc-400">
        <span>
          {waterfall.count} span{waterfall.count === 1 ? "" : "s"}
        </span>
        {cut > 0 && (
          <span className="text-amber-600 dark:text-amber-400">
            {cut} more not drawn — collapse a branch to reach them
          </span>
        )}
        <span className="ml-auto flex items-center gap-2">
          {collapsible.length > 0 && (
            <button
              type="button"
              onClick={() =>
                setCollapsed((prev) => (prev.size ? new Set() : new Set(collapsible)))
              }
              className="rounded px-1.5 py-0.5 hover:bg-black/[0.06] dark:hover:bg-white/[0.08]"
            >
              {collapsed.size ? "Expand all" : "Collapse all"}
            </button>
          )}
          {!fitted && (
            <button
              type="button"
              onClick={fit}
              className="rounded px-1.5 py-0.5 hover:bg-black/[0.06] dark:hover:bg-white/[0.08]"
            >
              Fit (Esc)
            </button>
          )}
        </span>
      </div>

      {/* One scroller for both axes. The ruler is sticky at the top *inside* it
          so it scrolls sideways in lockstep with the bars it measures, and the
          corner above the pinned name column is sticky in both directions so
          neither the ruler nor a row slides out from under it. */}
      <div
        ref={scroller}
        className="min-h-0 flex-1 overflow-auto overscroll-x-contain"
      >
        <div style={{ width: LABEL_PX + trackPx }}>
          <div className="sticky top-0 z-20 flex bg-white dark:bg-zinc-900">
            <div
              style={{ width: LABEL_PX }}
              className="sticky left-0 z-30 shrink-0 border-b border-black/10 bg-white dark:border-white/10 dark:bg-zinc-900"
            />
            <WaterfallAxis
              view={view}
              context={context}
              scale={scale}
              trackPx={trackPx}
              scroller={scroller}
              onChange={apply}
              onFit={fit}
            />
          </div>

          <div
            role="treegrid"
            aria-label="Trace waterfall"
            aria-rowcount={waterfall.count}
            aria-activedescendant={keys.activeId}
            onKeyDown={keys.onKeyDown}
            tabIndex={0}
            className="outline-none focus-visible:ring-1 focus-visible:ring-sky-500/40"
          >
            {rows.map((row) => (
              <WaterfallRow
                key={row.node.id}
                row={row}
                scale={scale}
                trackPx={trackPx}
                selected={row.node.id === selectedId}
                active={rowElementId(row.node.id) === keys.activeId}
                onSelect={() => onSelect(row.node)}
                onToggle={() => toggle(row.node.id)}
              />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
