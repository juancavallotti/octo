"use client";

import { useEffect, useState } from "react";
import {
  readDeploymentOverview,
  type StatsPod,
  type StatsSeriesPage,
} from "@/app/model/stats";
import { reachFor, type View } from "./range";

/**
 * One deployment's CPU and memory over a window, plus the state of the pods that
 * reported them.
 *
 * Two reads, answered in one round trip, because they answer different
 * questions and fail differently. The series is the chart; the pod list is what
 * explains an empty chart — a deployment with no pods in the index has its sidecar switched off,
 * while a deployment with pods and no rows simply has nothing in this window, and
 * those two want different words on screen.
 *
 * `loading` is derived from a comparison of what is wanted with what is loaded
 * rather than set and cleared, so a superseded request cannot leave a spinner
 * running when its replacement has already landed.
 */

export interface DeploymentMetrics {
  series: StatsSeriesPage | null;
  pods: StatsPod[];
  /**
   * How far back this view asked, deduced from the pods' own configuration.
   * Exposed so every reader sizes to the same window rather than each deriving
   * it and drifting.
   */
  askMs: number;
  loading: boolean;
  error: string | null;
}

export function useDeploymentMetrics(
  deploymentId: string,
  view: View,
  /** The moment the window ends. State in the caller, not Date.now() here: a
   * window that moves every render is a new query every render. */
  now: number,
): DeploymentMetrics {
  const [series, setSeries] = useState<StatsSeriesPage | null>(null);
  const [pods, setPods] = useState<StatsPod[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<string | null>(null);

  // The same deduction the server made from the same pods, so the axis is sized
  // to the window that was asked for. Before the first answer it is the view's
  // guess.
  const askMs = reachFor(view, pods);

  const wanted = `${deploymentId} ${view} ${now}`;

  useEffect(() => {
    let cancelled = false;

    void (async () => {
      try {
        const overview = await readDeploymentOverview(deploymentId, view, now);
        if (cancelled) return;

        // Whichever half landed is kept: a pods read that fails should not throw
        // away a series that arrived. Only a total failure is an error worth the
        // empty state; a partial one is reported by the strip.
        if (overview.series) setSeries(overview.series);
        if (overview.pods) setPods(overview.pods);
        setError(overview.error);
      } catch (err) {
        if (cancelled) return;
        // The last good data stays on screen. A transient failure should not
        // blank a chart somebody is in the middle of reading.
        setError(message(err));
      } finally {
        if (!cancelled) setLoaded(wanted);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [deploymentId, view, now, wanted]);

  return { series, pods, askMs, loading: loaded !== wanted, error };
}

/** An unknown rejection as a sentence. */
function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
