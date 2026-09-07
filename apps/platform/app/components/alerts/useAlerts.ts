"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import {
  listIncidents,
  listWatches,
  type Incident,
  type WatchListItem,
} from "@/app/model/alerts";

/**
 * The alerts tab's data: every watch with its state, and whatever is open.
 *
 * It polls rather than streaming. A watch is evaluated at most once a minute, so
 * a socket would spend its life idle to deliver news already a minute old — and
 * the SSE plumbing this app has is for events the orchestrator raises, which
 * alerting is not.
 *
 * Modelled on useDeploymentStats, including the sequence guard: two polls overlap
 * whenever one is slower than the interval, and the older one finishing last
 * would put a stale list back on the page.
 */

/** Half the shortest evaluation interval, so a change shows up within a tick. */
const POLL_MS = 30_000;

export interface AlertsData {
  /** Null until the first load resolves, so the page can tell it apart from empty. */
  watches: WatchListItem[] | null;
  incidents: Incident[];
  error: string | null;
  /** Re-read now, for after something has been changed. */
  reload: () => Promise<void>;
}

export function useAlerts(): AlertsData {
  const [watches, setWatches] = useState<WatchListItem[] | null>(null);
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [error, setError] = useState<string | null>(null);
  const sequence = useRef(0);

  const read = useCallback(async () => {
    const mine = ++sequence.current;
    try {
      const [w, i] = await Promise.all([
        listWatches(),
        listIncidents({ open: true, limit: 20 }),
      ]);
      if (sequence.current !== mine) return;
      setWatches(w);
      setIncidents(i);
      setError(null);
    } catch (e) {
      if (sequence.current !== mine) return;
      // The last good list is kept: a transient blip should not empty the page
      // and read as "no watches", which is a very different thing to see.
      setError((e as Error).message);
    }
  }, []);

  useEffect(() => {
    let stopped = false;
    const poll = async () => {
      if (stopped) return;
      await read();
    };
    void poll();
    const timer = setInterval(() => void poll(), POLL_MS);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [read]);

  return { watches, incidents, error, reload: read };
}
