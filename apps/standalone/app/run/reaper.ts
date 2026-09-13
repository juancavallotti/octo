import { stop } from "./localRunner";
import { allSessions } from "./session";

/**
 * Idle-run reaper: a namespaced run holds an octo process and a pooled port, so a
 * namespace with no activity within a rolling one-hour window is stopped and its port
 * freed. Any manager call renews the window (see session()), so only a genuinely idle
 * run ages out; SSE keepalive pings are server-to-client and renew nothing.
 */

/** Rolling inactivity window before a run is stopped and cleared. */
const ACTIVITY_TIMEOUT_MS = 60 * 60 * 1000;
/** How often the reaper sweeps. */
const REAPER_INTERVAL_MS = 60 * 1000;

const store = globalThis as unknown as {
  __octoRunReaper?: ReturnType<typeof setInterval>;
};

/**
 * Stop and forget every run whose namespace has been idle past the timeout.
 * Exported for tests; the interval calls it. `now` is injectable so tests don't
 * have to wait an hour. The delete is unconditional after stop: stop() renews the
 * timestamp as a side effect, but a run we already decided to reap stays reaped.
 */
export async function reapIdle(now: number = Date.now()): Promise<void> {
  const expired = [...allSessions().entries()].filter(
    ([, s]) => now - s.lastActivity > ACTIVITY_TIMEOUT_MS,
  );
  for (const [ns, s] of expired) {
    s.logs.push("⏲ run idle for 1h — stopping and clearing");
    await stop(ns);
    allSessions().delete(ns);
  }
}

/** Start the single reaper interval (idempotent). Unref'd so it never keeps the
 * process alive on its own. */
export function ensureReaper(): void {
  if (store.__octoRunReaper) return;
  const timer = setInterval(() => {
    // A sweep that throws (a stubborn stop, say) must not surface as an unhandled
    // rejection: Node turns those into a process-ending exception, so a single bad
    // sweep would take the whole editor down. Log it and let the next tick try again.
    reapIdle().catch((err) => {
      console.error("[octo] idle-run reaper sweep failed:", err);
    });
  }, REAPER_INTERVAL_MS);
  timer.unref?.();
  store.__octoRunReaper = timer;
}
