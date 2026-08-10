/**
 * The app list's data lifecycle: fetch the apps that produced traces in a window,
 * and keep the window the service actually measured over.
 *
 * The window is state rather than a constant because every count in the response
 * is meaningless without it — "12 traces" says nothing until you know whether
 * that is an hour or a week — and because the service, not this client, decides
 * what an unbounded query means. So the request goes out with no bounds by
 * default and the response's echoed from/to is what gets displayed.
 */

import { useCallback, useEffect, useState } from "react";
import { listTraceApps, type TraceApp, type TraceWindow } from "@/app/model/traces";

export interface TraceApps {
  apps: TraceApp[];
  /** The window the counts were measured over, as the service reported it back. */
  window: { from: string; to: string } | null;
  loading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useTraceApps(window: TraceWindow = {}): TraceApps {
  const [apps, setApps] = useState<TraceApp[]>([]);
  const [measured, setMeasured] = useState<{ from: string; to: string } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  const { from, to } = window;
  // What the current state was loaded for. Loading is derived from it rather than
  // stored, so a superseded request cannot leave a spinner running: whatever the
  // effect is fetching now is by definition not yet what is on screen.
  const wanted = `${from ?? ""} ${to ?? ""} ${nonce}`;
  const [loaded, setLoaded] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    listTraceApps({ from, to }).then(
      (page) => {
        if (!live) return;
        setApps(page.items);
        setMeasured({ from: page.from, to: page.to });
        setError(null);
        setLoaded(wanted);
      },
      (err: Error) => {
        if (!live) return;
        // The list is left as it was: an app that was there a moment ago has not
        // stopped existing because one poll failed, and blanking it would read
        // as "nothing is being traced any more".
        setError(err.message);
        setLoaded(wanted);
      },
    );
    // An in-flight request whose window has already changed is abandoned rather
    // than allowed to land, so a slow first answer cannot overwrite a fast second.
    return () => {
      live = false;
    };
  }, [from, to, wanted]);

  const refresh = useCallback(() => setNonce((n) => n + 1), []);
  return { apps, window: measured, loading: loaded !== wanted, error, refresh };
}
