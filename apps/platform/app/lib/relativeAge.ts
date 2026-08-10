/**
 * Compact relative age (e.g. "3m", "2h", "5d") from an RFC3339 timestamp.
 *
 * Null rather than a placeholder for the three cases that are not an age: no
 * timestamp, an unparseable one, and one in the future. A caller decides what to
 * render for "we don't know", and the choices differ — a dash in a table, nothing
 * at all beside a label.
 *
 * `now` is a parameter so the function is testable without freezing the clock,
 * matching `components/traces/format.ts`. It defaults, so no caller has to care.
 */
export function relativeAge(iso?: string, now: number = Date.now()): string | null {
  if (!iso) return null;
  const secs = Math.floor((now - new Date(iso).getTime()) / 1000);
  if (!Number.isFinite(secs) || secs < 0) return null;
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}
