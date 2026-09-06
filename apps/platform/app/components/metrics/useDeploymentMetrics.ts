"use client";

import { useEffect, useState } from "react";
import {
  listStatsPods,
  readStatsSeries,
  type StatsPod,
  type StatsSeriesPage,
} from "@/app/model/stats";
import { CPU_METRIC, MEM_METRIC } from "@/app/components/stats/chart/metrics";
import { reachFor, viewPreset, windowFor, type View } from "./range";

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
  /**
   * How far back this view asked, deduced from the pods' own configuration.
   * Exposed so the chart can size its axis and the grid can ask for the same
   * window, rather than each deriving it and drifting.
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

  // Deduced from the pods rather than assumed, and from this hook's own state
  // rather than passed in: the pod list is not windowed, so it costs nothing to
  // read it before knowing how far back to look. The first poll has no pods and
  // falls back to the view's guess; every one after asks for exactly what the
  // tier holds.
  const askMs = reachFor(view, pods);

  const wanted = `${deploymentId} ${view} ${askMs} ${now}`;

  useEffect(() => {
    let cancelled = false;
    const window = windowFor(view, now, askMs);

    void (async () => {
      try {
        // Settled rather than all: the two calls answer different questions and
        // fail independently. A pods request that fails should not throw away a
        // series response that arrived — the chart is the point of the page, and
        // the pod table is what explains an empty one.
        const [nextSeries, nextPods] = await Promise.allSettled([
          readStatsSeries(deploymentId, {
            metrics: [CPU_METRIC, MEM_METRIC],
            // The view names its tier. Never auto: a window longer than the
            // live tier reaches would be answered entirely from buckets, which
            // is a hundred and twenty points becoming two without saying so.
            tier: viewPreset(view).tier,
            from: window.from,
            to: window.to,
            // The service clamps to 5000; 1500 is more points than the widest
            // chart has pixels, and asking for the cap would mostly move bytes.
            limit: 1500,
          }),
          listStatsPods(deploymentId),
        ]);
        if (cancelled) return;

        if (nextSeries.status === "fulfilled") setSeries(nextSeries.value);
        if (nextPods.status === "fulfilled") setPods(nextPods.value.items);

        // Only a total failure is an error. One half landing is a better page
        // than neither, and the half that did not is reported by what it would
        // have filled being empty.
        const failure =
          nextSeries.status === "rejected"
            ? nextSeries.reason
            : nextPods.status === "rejected"
              ? nextPods.reason
              : null;
        setError(failure === null ? null : message(failure));
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
  }, [deploymentId, view, askMs, now, wanted]);

  return { series, pods, askMs, loading: loaded !== wanted, error };
}

/** An unknown rejection as a sentence. */
function message(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
