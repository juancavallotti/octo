/**
 * The arithmetic behind canvas zoom.
 *
 * Kept apart from the components so every question the canvas asks — what the
 * next step is, where the scroll has to land to keep a point still, what factor
 * fits a flow in the window — is an ordinary function with ordinary tests, and so
 * none of it needs a DOM to answer.
 *
 * A "content point" here is a coordinate in the unzoomed flow board. Multiply it
 * by the zoom factor and you get the pixel it is drawn at; that one relationship
 * is what everything below is built out of.
 */

/** As far out as it goes: a whole large flow, still readable as a shape. */
export const ZOOM_MIN = 0.25;
/** As far in as it goes. Past 2 the pixels are the only thing getting bigger. */
export const ZOOM_MAX = 2;

/**
 * The stops a button press moves between.
 *
 * Steps rather than a multiplier so the control is predictable — pressing "−"
 * three times and "+" three times returns to exactly where it started, which a
 * repeated ×0.8/×1.25 does not.
 */
export const ZOOM_STEPS: readonly number[] = [
  0.25, 0.33, 0.5, 0.67, 0.75, 1, 1.25, 1.5, 1.75, 2,
];

export function clampZoom(zoom: number): number {
  if (!Number.isFinite(zoom)) return 1;
  return Math.min(Math.max(zoom, ZOOM_MIN), ZOOM_MAX);
}

/** The next stop above `zoom`, or `zoom` clamped when there is none. */
export function nextStep(zoom: number): number {
  const found = ZOOM_STEPS.find((step) => step > zoom + 1e-6);
  return found ?? ZOOM_MAX;
}

/** The next stop below `zoom`, or `zoom` clamped when there is none. */
export function prevStep(zoom: number): number {
  for (let i = ZOOM_STEPS.length - 1; i >= 0; i--) {
    if (ZOOM_STEPS[i] < zoom - 1e-6) return ZOOM_STEPS[i];
  }
  return ZOOM_MIN;
}

/** A scroll offset paired with the pointer position it was measured against. */
export interface Anchor {
  /** How far the surface is scrolled, in drawn pixels. */
  scroll: number;
  /** Where the pointer is, in drawn pixels from the surface's visible edge. */
  pointer: number;
}

/**
 * The content point currently under the pointer.
 *
 * Read *before* the zoom changes and written back after, this is what makes
 * zooming feel like it happens under the cursor rather than at the corner.
 */
export function contentPointAt({ scroll, pointer }: Anchor, zoom: number): number {
  return zoom > 0 ? (scroll + pointer) / zoom : 0;
}

/** Where to scroll so `point` lands back under `pointer` at the new zoom. */
export function scrollToHold(point: number, pointer: number, zoom: number): number {
  // Negative scroll is not an error to report, it is a surface that cannot move
  // any further left — which is what 0 means to a scroller.
  return Math.max(point * zoom - pointer, 0);
}

/**
 * The largest factor at which content `contentPx` wide fits in `viewportPx`.
 *
 * Capped at 1: fitting is for a flow that has outgrown the window, and blowing a
 * three-block flow up to fill a wide screen is not what anyone means by it.
 */
export function fitZoom(contentPx: number, viewportPx: number, pad = 32): number {
  if (!(contentPx > 0) || !(viewportPx > 0)) return 1;
  return clampZoom(Math.min(1, (viewportPx - pad) / contentPx));
}
