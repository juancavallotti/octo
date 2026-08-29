"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { clampZoom, nextStep, prevStep } from "./zoom";

/**
 * How far the flow canvas is zoomed, and whether a drag is in flight.
 *
 * It is context rather than local state in Canvas because three components need
 * the same number and none of them contains the others: the canvas draws at it,
 * NodeShell divides a drag translate by it, and the drag overlay is rendered
 * outside the zoomed surface and has to be scaled to match.
 *
 * It is mounted at EditorRoot rather than at EditorBody on purpose. EditorBody
 * returns early for the YAML, Resources and Testing views, so a provider there
 * would unmount every time someone looked at the YAML and hand them back a
 * canvas at 100% — losing a setting they chose because a flow is too big to read
 * at 100%.
 *
 * It is not in the editor reducer either: that reducer holds the document and
 * what is selected in it, and zoom is neither. It is also not persisted, which
 * keeps the preview route's screenshots byte-stable without the harness having
 * to know this exists.
 */
export interface CanvasZoom {
  zoom: number;
  setZoom: (zoom: number) => void;
  zoomIn: () => void;
  zoomOut: () => void;
  reset: () => void;
  /**
   * Whether a block is being dragged right now.
   *
   * While it is true every change above is a no-op: dnd-kit measures its drop
   * targets once, at the start of a drag, and resizing them underneath it would
   * leave the block landing somewhere other than where it was let go. The rule
   * is enforced here rather than at each gesture because there are four ways in
   * — buttons, wheel, shortcuts, and a keyboard drag that leaves the pointer
   * free to reach the buttons — and three of them would have to remember.
   */
  dragging: boolean;
  setDragging: (dragging: boolean) => void;
}

const ZoomContext = createContext<CanvasZoom | null>(null);

export function CanvasZoomProvider({ children }: { children: ReactNode }) {
  const [zoom, setZoomRaw] = useState(1);
  const [dragging, setDragging] = useState(false);

  // Read through a ref so the gate does not have to be a dependency of every
  // callback below — a drag starting must not hand the canvas new function
  // identities and re-register its listeners mid-gesture.
  const locked = useRef(false);
  locked.current = dragging;
  const change = useCallback((next: (z: number) => number) => {
    if (locked.current) return;
    setZoomRaw((z) => clampZoom(next(z)));
  }, []);

  const value = useMemo<CanvasZoom>(
    () => ({
      zoom,
      setZoom: (next: number) => change(() => next),
      zoomIn: () => change(nextStep),
      zoomOut: () => change(prevStep),
      reset: () => change(() => 1),
      dragging,
      setDragging,
    }),
    [zoom, change, dragging],
  );

  return <ZoomContext.Provider value={value}>{children}</ZoomContext.Provider>;
}

/**
 * The canvas zoom.
 *
 * Falls back to a fixed 1 outside the provider rather than throwing, so a
 * component that happens to be rendered in isolation — a test, a story — draws
 * at its natural size instead of failing.
 */
export function useCanvasZoom(): CanvasZoom {
  return useContext(ZoomContext) ?? FIXED;
}

const FIXED: CanvasZoom = {
  zoom: 1,
  setZoom: () => {},
  zoomIn: () => {},
  zoomOut: () => {},
  reset: () => {},
  dragging: false,
  setDragging: () => {},
};
