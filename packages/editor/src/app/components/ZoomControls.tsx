"use client";

import { Maximize2, Minus, Plus } from "lucide-react";
import { useCanvasZoom } from "../canvas/ZoomContext";
import { ZOOM_MAX, ZOOM_MIN } from "../canvas/zoom";

/**
 * The zoom control, pinned to the bottom-right of the canvas.
 *
 * Bottom-right because top-left already holds the connections, env and resources
 * launchers, and because that is the corner a canvas control is looked for in.
 * It sits outside the scrolling surface, so it does not scroll away and is not
 * itself drawn at the zoom it sets.
 *
 * The percentage is a button: someone who has zoomed somewhere odd wants one
 * click back to 100%, and giving that its own labelled control would be a third
 * button for something the number already names.
 */
export default function ZoomControls({ onFit }: { onFit: () => void }) {
  const { zoom, zoomIn, zoomOut, reset } = useCanvasZoom();

  return (
    <div className="absolute bottom-4 right-4 z-30 flex items-center gap-0.5 rounded-lg border border-black/10 bg-white/90 p-0.5 shadow-sm backdrop-blur-sm dark:border-white/10 dark:bg-zinc-900/90">
      <Button label="Zoom out" onClick={zoomOut} disabled={zoom <= ZOOM_MIN}>
        <Minus size={14} />
      </Button>

      <button
        type="button"
        onClick={reset}
        title="Reset to 100%"
        aria-label={`Zoom ${Math.round(zoom * 100)}%, reset to 100%`}
        className="min-w-12 rounded px-1 py-1 text-center font-mono text-xs tabular-nums text-zinc-500 transition-colors hover:bg-black/[0.06] hover:text-zinc-800 dark:text-zinc-400 dark:hover:bg-white/[0.08] dark:hover:text-zinc-100"
      >
        {Math.round(zoom * 100)}%
      </button>

      <Button label="Zoom in" onClick={zoomIn} disabled={zoom >= ZOOM_MAX}>
        <Plus size={14} />
      </Button>

      <Button label="Fit flows in view" onClick={onFit}>
        <Maximize2 size={13} />
      </Button>
    </div>
  );
}

function Button({
  label,
  onClick,
  disabled,
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      className="rounded p-1.5 text-zinc-500 transition-colors hover:bg-black/[0.06] hover:text-zinc-800 disabled:opacity-40 disabled:hover:bg-transparent dark:text-zinc-400 dark:hover:bg-white/[0.08] dark:hover:text-zinc-100"
    >
      {children}
    </button>
  );
}
