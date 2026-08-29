"use client";

import { useCallback, useEffect, useLayoutEffect, useRef } from "react";
import FlowBoard from "./FlowBoard";
import ConnectionsLauncher from "./ConnectionsLauncher";
import EnvLauncher from "./EnvLauncher";
import ResourcesLauncher from "./ResourcesLauncher";
import ZoomControls from "./ZoomControls";
import { useCanvasZoom } from "../canvas/ZoomContext";
import { contentPointAt, fitZoom, scrollToHold } from "../canvas/zoom";

/**
 * Canvas is the main flow-editing area: a scrollable dot-grid surface that hosts
 * all the file's flows stacked vertically (FlowBoard). The connections launcher is
 * pinned to the top-left as an overlay outside the scroll area, so it stays put as
 * the flows scroll, and the zoom control sits opposite it at the bottom-right.
 *
 * Zoom is CSS `zoom` on a layer inside the scroller, not `transform: scale`. That
 * choice carries the whole feature: `zoom` affects layout, so the scrollable area
 * grows and shrinks with the drawn flows, `mx-auto` keeps centring the board when
 * it is smaller than the window, and `getBoundingClientRect()` keeps reporting
 * where things actually are — which is what lets dnd-kit measure drop targets
 * correctly with no correction at all. A transform would leave the scroller
 * believing the content is still its original size and need a mirrored sizer
 * element to fake the difference.
 */
export default function Canvas() {
  const { zoom, setZoom, dragging } = useCanvasZoom();
  const scroller = useRef<HTMLElement>(null);
  const layer = useRef<HTMLDivElement>(null);

  // Where the pointer was pointing, in content coordinates, so the scroll can be
  // put back after the browser has re-laid the surface out at the new zoom. It
  // cannot be done in the same tick as the zoom change: the scroll extents that
  // would clamp it do not exist yet.
  const hold = useRef<{ x: number; y: number; px: number; py: number } | null>(null);

  useLayoutEffect(() => {
    const element = scroller.current;
    const target = hold.current;
    hold.current = null;
    if (!element || !target) return;
    element.scrollLeft = scrollToHold(target.x, target.px, zoom);
    element.scrollTop = scrollToHold(target.y, target.py, zoom);
  }, [zoom]);

  // Registered by hand rather than with an onWheel prop because React attaches
  // wheel listeners passively: preventDefault() from a synthetic handler is
  // ignored, so the browser would zoom the whole page underneath the canvas. A
  // native listener with { passive: false } is the only way to hold it still.
  //
  // Modified wheel only — a bare wheel has to keep scrolling the flows. A
  // trackpad pinch arrives as a ctrl-wheel, which is exactly the gesture wanted.
  useEffect(() => {
    const element = scroller.current;
    if (!element) return;
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return;
      e.preventDefault();
      if (dragging) return;
      const box = element.getBoundingClientRect();
      const px = e.clientX - box.left;
      const py = e.clientY - box.top;
      hold.current = {
        x: contentPointAt({ scroll: element.scrollLeft, pointer: px }, zoom),
        y: contentPointAt({ scroll: element.scrollTop, pointer: py }, zoom),
        px,
        py,
      };
      setZoom(zoom * (e.deltaY > 0 ? 0.9 : 1.1));
    };
    element.addEventListener("wheel", onWheel, { passive: false });
    return () => element.removeEventListener("wheel", onWheel);
  }, [zoom, setZoom, dragging]);

  // Measured off the layer rather than tracked: under `zoom` a measured rect is
  // in drawn pixels, so dividing by the current factor recovers the natural width
  // without keeping a second copy of it in state.
  const fit = useCallback(() => {
    const element = scroller.current;
    const content = layer.current;
    if (!element || !content) return;
    const natural = content.getBoundingClientRect().width / zoom;
    setZoom(fitZoom(natural, element.clientWidth));
  }, [zoom, setZoom]);

  useCanvasZoomShortcuts({ fit, disabled: dragging });

  return (
    <div className="relative flex-1 min-w-0">
      <main
        ref={scroller}
        className="absolute inset-0 overflow-auto canvas-grid"
        // The grid is painted on the unzoomed scroller, so it does not scale with
        // the content on its own — the cell size follows the factor by hand.
        style={{ "--canvas-zoom": zoom } as React.CSSProperties}
      >
        <div ref={layer} style={{ zoom }}>
          <FlowBoard />
        </div>
      </main>
      <div className="absolute left-4 top-4 z-30 flex items-start gap-2">
        <ConnectionsLauncher />
        <EnvLauncher />
        <ResourcesLauncher />
      </div>
      <ZoomControls onFit={fit} />
    </div>
  );
}

/**
 * The keyboard shortcuts, bound while the canvas is on screen.
 *
 * On the document rather than on the surface, because zoom is about the view and
 * not about what has focus in it — and scoped by this hook living in Canvas,
 * which is only mounted in the canvas view mode. preventDefault is not optional:
 * without it the browser page-zooms as well, and the editor ends up drawn twice
 * as large inside a canvas drawn twice as small.
 */
function useCanvasZoomShortcuts({
  fit,
  disabled,
}: {
  fit: () => void;
  disabled: boolean;
}) {
  const { zoomIn, zoomOut, reset } = useCanvasZoom();

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (disabled) return;
      // The canvas is full of inline name fields, and "-" is a character in a
      // block name before it is a command.
      const target = e.target as HTMLElement | null;
      if (
        target?.isContentEditable ||
        target instanceof HTMLInputElement ||
        target instanceof HTMLTextAreaElement
      ) {
        return;
      }

      if (e.shiftKey && e.key === "!") {
        e.preventDefault();
        fit();
        return;
      }
      if (!e.ctrlKey && !e.metaKey) return;

      switch (e.key) {
        case "=":
        case "+":
          e.preventDefault();
          zoomIn();
          break;
        case "-":
        case "_":
          e.preventDefault();
          zoomOut();
          break;
        case "0":
          e.preventDefault();
          reset();
          break;
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [zoomIn, zoomOut, reset, fit, disabled]);
}
