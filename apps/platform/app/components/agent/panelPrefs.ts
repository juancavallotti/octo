/**
 * Where the chat panel sits, remembered between visits.
 *
 * Two preferences: whether the panel is docked (the page shrinks beside it) or
 * floating over the page, and how wide it is. Both are workspace-wide rather
 * than per-page — somebody who pins the panel wants it pinned everywhere.
 *
 * Deliberately *not* stored: whether the panel is open. The drawer is a dynamic
 * import (a Markdown renderer rides along with it), and restoring "open" would
 * pull that weight into first paint on every page load. A reload also kills any
 * answer in flight, so reopening onto a half-finished run would be a lie.
 *
 * Pure and React-free so it can be tested directly.
 */

const DOCKED_KEY = "octo.agent.docked";
const WIDTH_KEY = "octo.agent.width";

export const MIN_WIDTH = 320;
export const MAX_WIDTH = 720;
/** 32rem — the width the panel had before it was adjustable. */
export const DEFAULT_WIDTH = 512;

/**
 * localStorage can be absent (a non-browser test env) or throw on access (a
 * sandboxed frame, storage disabled by the user). A remembered layout is a
 * nicety, never worth crashing the panel — and the page around it — over, so
 * every path through here falls back to the default instead of propagating.
 */
function read(key: string): string | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage?.getItem(key) ?? null;
  } catch {
    return null;
  }
}

function write(key: string, value: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage?.setItem(key, value);
  } catch {
    // Blocked storage: the setting holds for this session and is forgotten.
  }
}

/** Clamp a width to the draggable range. */
export function clampWidth(n: number): number {
  if (!Number.isFinite(n)) return DEFAULT_WIDTH;
  return Math.min(MAX_WIDTH, Math.max(MIN_WIDTH, Math.round(n)));
}

/** Whether the panel docks beside the page. Floating unless it was pinned. */
export function readDocked(): boolean {
  return read(DOCKED_KEY) === "true";
}

export function writeDocked(docked: boolean): void {
  write(DOCKED_KEY, String(docked));
}

/** The stored panel width, clamped; DEFAULT_WIDTH when absent or malformed. */
export function readWidth(): number {
  const raw = read(WIDTH_KEY);
  if (raw === null) return DEFAULT_WIDTH;
  const n = Number(raw);
  if (!Number.isFinite(n)) return DEFAULT_WIDTH;
  return clampWidth(n);
}

export function writeWidth(width: number): void {
  write(WIDTH_KEY, String(clampWidth(width)));
}
