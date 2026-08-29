/**
 * Keyboard navigation for the waterfall's treegrid.
 *
 * `role="treegrid"` is a promise about behaviour, not a source of it: it tells
 * assistive technology that arrow keys will walk a tree, and then something has
 * to actually walk it. Without this the role is a claim the widget does not keep.
 *
 * The active row is tracked here and pointed at with `aria-activedescendant`,
 * rather than moving real focus between rows. The rows are plain divs rebuilt on
 * every zoom, and focus that has to survive that is focus that gets lost.
 *
 * Scrolling the active row into view is deliberately *not* here. A row is as
 * wide as the whole track now, and what a scroll into view must not disturb —
 * the horizontal position of the chart — is the caller's scroller, not this
 * hook's business.
 *
 * Keys follow the ARIA treegrid pattern — arrows walk and fold the tree, Enter
 * opens a span — with the chart's own two additions: shifted arrows pan the
 * viewport, and Escape fits the whole trace back on screen.
 */

import { useCallback, useState } from "react";
import type { WaterfallRowModel } from "./chartLayout";

export interface TreegridKeys {
  /** The row the keyboard is on, or undefined when there are none. */
  activeId: string | undefined;
  onKeyDown: (e: React.KeyboardEvent) => void;
}

export function useTreegridKeys(
  rows: WaterfallRowModel[],
  select: (row: WaterfallRowModel) => void,
  toggle: (row: WaterfallRowModel) => void,
  pan: (direction: -1 | 1) => void,
  reset: () => void,
): TreegridKeys {
  const [active, setActive] = useState(0);

  // Folding a branch away can leave the cursor past the end, so it is clamped on
  // read rather than corrected by an effect after the fact.
  const index = rows.length === 0 ? -1 : Math.min(active, rows.length - 1);
  const row = index < 0 ? undefined : rows[index];

  const onKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      const move = (to: number) => {
        e.preventDefault();
        setActive(Math.min(Math.max(to, 0), rows.length - 1));
      };

      switch (e.key) {
        case "ArrowDown":
          return move(index + 1);
        case "ArrowUp":
          return move(index - 1);
        case "Home":
          return move(0);
        case "End":
          return move(rows.length - 1);

        case "ArrowRight":
          e.preventDefault();
          if (e.shiftKey) return pan(1);
          // Open a folded branch; on an open or childless row, step inward —
          // which for a leaf is simply the next row down.
          if (row?.expandable && row.collapsed) return toggle(row);
          return move(index + 1);

        case "ArrowLeft":
          e.preventDefault();
          if (e.shiftKey) return pan(-1);
          if (row?.expandable && !row.collapsed) return toggle(row);
          return move(parentOf(rows, index));

        case "Enter":
        case " ":
          e.preventDefault();
          if (row) select(row);
          return;

        case "Escape":
          e.preventDefault();
          return reset();
      }
    },
    [index, row, rows, select, toggle, pan, reset],
  );

  return { activeId: row && rowElementId(row.node.id), onKeyDown };
}

/** The DOM id of a row, so the container can point at it. */
export function rowElementId(nodeId: string): string {
  return `wf-${nodeId}`;
}

/** The nearest row above `index` that is one level shallower — the parent a
 * left-arrow steps out to. Falls back to staying put at the top level. */
function parentOf(rows: WaterfallRowModel[], index: number): number {
  const depth = rows[index]?.node.depth ?? 0;
  for (let i = index - 1; i >= 0; i--) {
    if (rows[i].node.depth < depth) return i;
  }
  return index;
}
