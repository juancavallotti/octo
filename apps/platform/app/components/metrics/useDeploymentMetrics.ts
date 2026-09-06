"use client";

import { useEffect, useState } from "react";
import {
  listStatsPods,
  readStatsSeries,
  type StatsPod,
  type StatsSeriesPage,
} from "@/app/model/stats";
import { CPU_METRIC, MEM_METRIC } from "@/app/components/stats/chart/metrics";
import { windowFor, type RangeKey } from "./range";

/**
 * One deployment's CPU and memory over a window, plus the state of the pods that
 * reported them.
 *
 * Two calls rather than one because they answer different questions and fail
 * differently. The series is the chart; the pod list is what explains an empty
 * chart — a deployment with no pods in the index has its sidecar switched off,
 * while a deployment with pods and no rows simply has nothing in this window, and
 * those two want different words on screen.
 *
 * Staleness is handled with the wanted/loaded idiom the trace hooks use: `loading`
 * is derived from a comparison rather than set and cleared, so a superseded
 * request cannot leave a spinner running when its replacement has already landed.
 */

export interface DeploymentMetrics {
  series: StatsSeriesPage | null;
  pods: StatsPod[];
  loading: boolean;
  error: string | null;
}

export function useDeploymentMetrics(
  deploymentId: string,
  range: RangeKey,
  /** The moment the window ends. State in the caller, not Date.now() here: a
   * window that moves every render is a new query every render. */
  now: number,
): DeploymentMetrics {
  const [series, setSeries] = useState<StatsSeriesPage | null>(null);
  const [pods, setPods] = useState<StatsPod[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loaded, setLoaded] = useState<string | null>(null);

  const wanted = `${deploymentId} ${range} ${now}`;

  useEffect(() => {
    let cancelled = false;
    const window = windowFor(range, now);

    void (async () => {
      try {
        const [nextSeries, nextPods] = await Promise.all([
          readStatsSeries(deploymentId, {
            metrics: [CPU_METRIC, MEM_METRIC],
            tier: "auto",
            from: window.from,
            to: window.to,
            // The service clamps to 5000; 1500 is more points than the widest
            // chart has pixels, and asking for the cap would mostly move bytes.
            limit: 1500,
          }),
          listStatsPods(deploymentId),
        ]);
        if (cancelled) return;
        setSeries(nextSeries);
        setPods(nextPods.items);
        setError(null);
      } catch (err) {
        if (cancelled) return;
        // The last good data stays on screen. A transient failure should not
        // blank a chart somebody is in the middle of reading.
        setError(err instanceof Error ? err.message : String(err));
      } finally {
        if (!cancelled) setLoaded(wanted);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [deploymentId, range, now, wanted]);

  return { series, pods, loading: loaded !== wanted, error };
}
