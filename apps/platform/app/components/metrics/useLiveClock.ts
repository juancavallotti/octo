"use client";

import { useCallback, useEffect, useState } from "react";

/**
 * The moment a metrics view's window ends, advanced on a timer.
 *
 * A clock rather than `Date.now()` read during render, for the same reason the
 * page held one before it polled: the window is a query parameter, so a moment
 * that changes on every render is a refetch on every render. This makes the
 * change deliberate and countable.
 *
 * Paused, it holds the moment it was paused at. That is the point of pausing —
 * a reader who has found a spike wants it to stay on screen while they look at
 * the rest of the page, not to watch it slide off the left edge.
 */
export function useLiveClock(
  intervalMs: number,
  live: boolean,
): { now: number; refresh: () => void } {
  const [now, setNow] = useState(() => Date.now());

  const refresh = useCallback(() => setNow(Date.now()), []);

  useEffect(() => {
    if (!live) return;
    const timer = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(timer);
  }, [intervalMs, live]);

  return { now, refresh };
}
