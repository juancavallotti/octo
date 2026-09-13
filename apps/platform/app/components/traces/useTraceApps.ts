/**
 * The app list's data lifecycle: fetch the apps that produced traces in a window.
 *
 * The window is always sent as explicit bounds, so a count is reported against a
 * window the caller already knows — nothing here echoes one back.
 */

import { useCallback, useEffect, useState } from "react";
import { listTraceApps, type TraceApp, type TraceWindow } from "@/app/model/traces";

export interface TraceApps {
  apps: TraceApp[];
  loading: boolean;
  error: string | null;
  refresh: () => void;
}

export function useTraceApps(window: TraceWindow = {}): TraceApps {
  const [apps, setApps] = useState<TraceApp[]>([]);
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
        setError(null);
        setLoaded(wanted);
      },
      (err: Error) => {
        if (!live) return;
        // The list is left as it was: an app has not stopped existing because one
        // poll failed.
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
  return { apps, loading: loaded !== wanted, error, refresh };
}
