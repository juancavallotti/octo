/**
 * Where the chat panel sits, remembered between visits: whether it is docked (the
 * page shrinks beside it) or floating, and how wide it is. Both are
 * workspace-wide rather than per-page.
 *
 * Not stored: whether the panel is open. Restoring that would pull a dynamic
 * import into first paint, and a reload kills any answer in flight anyway.
 */

const DOCKED_KEY = "octo.agent.docked";
const WIDTH_KEY = "octo.agent.width";

export const MIN_WIDTH = 320;
export const MAX_WIDTH = 720;
/** 32rem. */
export const DEFAULT_WIDTH = 512;

/**
 * localStorage can be absent (no browser) or throw on access (a sandboxed frame,
 * storage disabled). A remembered layout is never worth a crash, so every path
 * through here falls back to the default instead of propagating.
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
