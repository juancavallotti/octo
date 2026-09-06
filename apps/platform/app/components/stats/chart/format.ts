/**
 * Small presentational helpers shared by the chart and its readout.
 */

/**
 * A tick or readout label at the precision the window justifies: a clock while
 * the window is minutes or hours, a date once it spans days. Locale-formatted,
 * because these are wall-clock moments a reader compares against their own
 * memory of when something happened.
 */
export function formatClock(ms: number, spanMs: number): string {
  const at = new Date(ms);
  if (spanMs > 36 * 3_600_000) {
    return at.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  }
  return at.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

/** The tail of a pod name, which is the part that differs between two pods of
 * the same deployment — the prefix they share is the deployment's own id. */
export function shortPod(pod: string): string {
  return pod.length > 18 ? `…${pod.slice(-16)}` : pod;
}
