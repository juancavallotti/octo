import { allSessions, stop } from "./session";

/**
 * Idle-run reaper. A namespaced run holds an octo process and a pooled port; with
 * many users we can't keep them forever. This sweeps every namespace that has seen
 * no activity within a rolling one-hour window, stopping its process and freeing
 * its port. The window is renewed by any manager call (see session()), so an editor
 * that is being used — even just hot-reload syncs — never ages out; only a tab
 * left idle does. SSE keepalive pings are server→client only and don't renew it.
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
  const timer = setInterval(() => void reapIdle(), REAPER_INTERVAL_MS);
  timer.unref?.();
  store.__octoRunReaper = timer;
}
