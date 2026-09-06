"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { readStatsSeries } from "@/app/model/stats";
import {
  CPU_METRIC,
  MEM_METRIC,
  toCores,
  toGauge,
  type Points,
} from "@/app/components/stats/chart/metrics";
import { parseGoDuration } from "@/app/components/stats/chart/duration";

/**
 * The last five minutes of CPU and memory for a set of deployments, for the
 * sparklines on their cards.
 *
 * On its own cadence rather than the page's eight-second deployment poll, and the
 * reason is bytes: five minutes of one-second samples is about three hundred
 * points per metric per pod, so riding the status poll would move a hundred
 * kilobytes every eight seconds to animate a shape a hundred pixels wide. Thirty
 * seconds is well under the window, so nothing is missed.
 *
 * An install without the stats sidecar is the ordinary case here, not an error
 * case: it is off by default. When every deployment fails the same way, the hook
 * reports unavailable and the cards simply do not grow a sparkline row. Seven
 * error strips on a page nobody asked a question on would be worse than silence.
 */

const REFRESH_MS = 30_000;

/** Shared so the "nothing to ask about" return is a stable identity. */
const EMPTY: ReadonlyMap<string, SparkData> = new Map();
const WINDOW_MS = 5 * 60_000;

/** Both columns for one deployment, pods folded together. */
export interface SparkData {
  cpu: Points;
  memory: Points;
}

export interface DeploymentStats {
  data: ReadonlyMap<string, SparkData>;
  available: boolean;
}

export function useDeploymentStats(deploymentIds: string[]): DeploymentStats {
  const [data, setData] = useState<Map<string, SparkData>>(new Map());
  const [available, setAvailable] = useState(true);

  // Compared by value: the caller rebuilds this array on every poll, so its
  // identity changes constantly while its contents rarely do. Serialized rather
  // than joined, because a deployment id is opaque and a separator character
  // inside one would split it into two requests for deployments that do not
  // exist.
  const key = JSON.stringify(deploymentIds);
  const ids = useMemo(() => JSON.parse(key) as string[], [key]);

  // A monotonic sequence, so a response can be discarded when a newer one has
  // already landed. It counts every poll, not every deployment set: two polls of
  // the same set overlap whenever one is slower than the interval, and the older
  // one finishing last would put a stale window back on the cards.
  const sequence = useRef(0);

  useEffect(() => {
    if (ids.length === 0) return;

    let stopped = false;

    const poll = async () => {
      const mine = ++sequence.current;
      const to = new Date();
      const from = new Date(to.getTime() - WINDOW_MS);

      const settled = await Promise.allSettled(
        ids.map((id) =>
          readStatsSeries(id, {
            metrics: [CPU_METRIC, MEM_METRIC],
            tier: "live",
            from: from.toISOString(),
            to: to.toISOString(),
          }).then((page) => [id, fold(page)] as const),
        ),
      );
      if (stopped || sequence.current !== mine) return;

      const next = new Map<string, SparkData>();
      let succeeded = 0;
      for (const outcome of settled) {
        if (outcome.status !== "fulfilled") continue;
        succeeded++;
        const [id, spark] = outcome.value;
        if (spark) next.set(id, spark);
      }

      // Last good data is kept on a total failure: a transient blip should not
      // make every card twitch.
      setAvailable(succeeded > 0);
      if (succeeded > 0) setData(next);
    };

    void poll();
    const timer = setInterval(() => void poll(), REFRESH_MS);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [ids]);

  // Derived rather than cleared in the effect: with nothing to ask about there is
  // nothing to show, and holding the previous answer would draw sparklines for
  // deployments that are no longer on the page.
  return { data: ids.length === 0 ? EMPTY : data, available };
}

/** Reduce one deployment's response to the two columns a sparkline draws.
 *
 * Pods are folded rather than drawn separately: at this size two overlaid lines
 * of the same colour are one thicker line. CPU sums, because two pods each using
 * half a core is a deployment using one; memory sums for the same reason. */
function fold(page: Awaited<ReturnType<typeof readStatsSeries>>): SparkData | null {
  const stepMs = parseGoDuration(page.step) ?? 1000;

  let cpu: Points | null = null;
  let memory: Points | null = null;
  for (const series of page.series) {
    if (series.name === CPU_METRIC) {
      cpu = add(cpu, toCores(series, stepMs));
    } else if (series.name === MEM_METRIC) {
      memory = add(memory, toGauge(series));
    }
  }
  if (!cpu && !memory) return null;
  return { cpu: cpu ?? empty(), memory: memory ?? empty() };
}

function empty(): Points {
  return { times: [], values: [] };
}

/**
 * Add one pod's column into the running total, aligned on time.
 *
 * A pod that has no reading at a moment contributes nothing rather than a zero,
 * and a moment where no pod reported stays a gap. Treating an absent pod as zero
 * would draw a cliff every time one restarted.
 */
function add(into: Points | null, next: Points): Points {
  if (!into) return next;

  const totals = new Map<number, number | null>();
  for (const source of [into, next]) {
    for (let i = 0; i < source.times.length; i++) {
      const value = source.values[i];
      const at = source.times[i];
      const running = totals.get(at);
      if (value === null) {
        if (running === undefined) totals.set(at, null);
        continue;
      }
      totals.set(at, (running ?? 0) + value);
    }
  }

  const times = [...totals.keys()].sort((a, b) => a - b);
  return { times, values: times.map((at) => totals.get(at) ?? null) };
}
