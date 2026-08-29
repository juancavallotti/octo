"use client";

import {
  createContext,
  useCallback,
  useContext,
  useMemo,
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
   * Zoom gestures sit still while it is true: dnd-kit measures its drop targets
   * once at the start of a drag, and resizing them underneath it would leave the
   * block landing somewhere other than where it was let go.
   */
  dragging: boolean;
  setDragging: (dragging: boolean) => void;
}

const ZoomContext = createContext<CanvasZoom | null>(null);

export function CanvasZoomProvider({ children }: { children: ReactNode }) {
  const [zoom, setZoomRaw] = useState(1);
  const [dragging, setDragging] = useState(false);

  const setZoom = useCallback((next: number) => setZoomRaw(clampZoom(next)), []);

  const value = useMemo<CanvasZoom>(
    () => ({
      zoom,
      setZoom,
      zoomIn: () => setZoomRaw((z) => nextStep(z)),
      zoomOut: () => setZoomRaw((z) => prevStep(z)),
      reset: () => setZoomRaw(1),
      dragging,
      setDragging,
    }),
    [zoom, setZoom, dragging],
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
