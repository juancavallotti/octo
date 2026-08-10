"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { TriangleAlert } from "lucide-react";
import { flattenWaterfall, panBy, zoomAt } from "./chartLayout";
import type { Interval } from "./timeSpans";
import type { Waterfall as WaterfallModel, WaterfallNode } from "./types";
import WaterfallAxis from "./WaterfallAxis";
import WaterfallRow from "./WaterfallRow";
import { rowElementId, useTreegridKeys } from "./useTreegridKeys";

/**
 * The chart: everything that happened on one trace, nested as it ran.
 *
 * The viewport is state and the tree is not — zooming, panning and collapsing
 * are arithmetic over a value that was built once, so none of them refetch.
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
  const bounds = useMemo<Interval>(
    () => ({ start: 0, end: waterfall.spanNs }),
    [waterfall.spanNs],
  );
  // A different trace is a different chart: the viewport and what was folded
  // away belong to the trace they were chosen on. That reset is done by keying
  // this component on the trace (see TraceDetail) rather than by an effect that
  // clears state after the fact — the state simply never carries over.
  const [view, setView] = useState<Interval>(bounds);
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

  // Zoom on a modified wheel only — a bare wheel has to keep scrolling the rows,
  // since a chart that hijacks it makes a long trace unreadable.
  //
  // Registered by hand rather than with an onWheel prop because React attaches
  // wheel listeners passively: preventDefault() from a synthetic handler is
  // ignored, so the page would zoom *and* scroll at once. A native listener with
  // { passive: false } is the only way to hold the scroll still.
  const scroller = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey && !e.altKey) return;
      e.preventDefault();
      const box = element.getBoundingClientRect();
      const fraction = box.width ? (e.clientX - box.left) / box.width : 0.5;
      setView((prev) => zoomAt(prev, fraction, e.deltaY > 0 ? 1.25 : 0.8, bounds));
    };
    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  }, [bounds]);

  const zoomed = view.start > bounds.start || view.end < bounds.end;

  // Arrow keys walk and fold the tree, shifted arrows pan, Escape fits. Scoped
  // to the chart rather than to the document: Escape belongs to whatever the
  // reader is actually in, and a chart that claims it globally takes it from
  // every dialog on the page.
  const keys = useTreegridKeys(
    rows,
    (row) => onSelect(row.node),
    (row) => toggle(row.node.id),
    useCallback(
      (direction: -1 | 1) => setView((prev) => panBy(prev, direction * 0.2, bounds)),
      [bounds],
    ),
    useCallback(() => setView(bounds), [bounds]),
  );

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
          {zoomed && (
            <button
              type="button"
              onClick={() => setView(bounds)}
              className="rounded px-1.5 py-0.5 hover:bg-black/[0.06] dark:hover:bg-white/[0.08]"
            >
              Fit (Esc)
            </button>
          )}
        </span>
      </div>

      {/* The ruler scrolls with the rows rather than sitting above them. Outside
          the scroller it would keep the container's full width while the rows
          lost a scrollbar's worth of it, and every bar would sit a few pixels
          off the tick it is measured against. Sticky also keeps it in view on a
          long trace, which is where reading a bar against it matters most. */}
      <div ref={scroller} className="min-h-0 flex-1 overflow-y-auto">
        <div className="sticky top-0 z-10 bg-white pl-64 pr-20 dark:bg-zinc-900">
          <WaterfallAxis view={view} bounds={bounds} onChange={setView} />
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
              view={view}
              selected={row.node.id === selectedId}
              active={rowElementId(row.node.id) === keys.activeId}
              onSelect={() => onSelect(row.node)}
              onToggle={() => toggle(row.node.id)}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
