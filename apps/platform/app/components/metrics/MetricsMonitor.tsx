"use client";

import { useMemo, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Gauge } from "lucide-react";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import LineChart, { type PodSeries } from "@/app/components/stats/chart/LineChart";
import { parseGoDuration } from "@/app/components/stats/chart/duration";
import {
  CPU_METRIC,
  MEM_METRIC,
  toCores,
  toGauge,
} from "@/app/components/stats/chart/metrics";
import type { StatsSeriesPage } from "@/app/model/stats";
import MetricGrid from "./MetricGrid";
import MetricsHeader from "./MetricsHeader";
import PodStatsTable from "./PodStatsTable";
import {
  GRID_REFRESH_FACTOR,
  readView,
  viewPreset,
  writeView,
  type View,
} from "./range";
import { useDeploymentMetrics } from "./useDeploymentMetrics";
import { useDeploymentCatalogue } from "./useDeploymentCatalogue";
import { useLiveClock } from "./useLiveClock";

/**
 * One deployment's CPU and memory, from the rolling week its pods keep in Redis.
 *
 * The range lives in the URL rather than in state, so a window worth showing
 * somebody is a link and a reload lands on what the reader was looking at. The
 * traces view argues the same thing at more length; the logs view mirrors state
 * out to the URL instead, and this is a vote.
 *
 * Replaced rather than pushed, matching how the traces filters navigate. A range
 * button is a filter, not a destination: pushing would make Back step through
 * every range the reader tried before it left the page.
 *
 * `now` is state and moves only when the reader asks. A window measured from
 * Date.now() on every render is a different query on every render, which is a
 * refetch loop rather than a live view.
 */
export default function MetricsMonitor({
  deploymentId,
  title,
}: {
  deploymentId: string;
  title?: string;
}) {
  const router = useRouter();
  const pathname = usePathname();
  const params = useSearchParams();
  const view = readView(new URLSearchParams(params.toString()));

  // Live by default. The window is a moving one, so the page should behave like
  // the thing it is showing.
  const [live, setLive] = useState(true);
  const beat = viewPreset(view).refreshMs;
  const { now, refresh } = useLiveClock(beat, live);
  const { now: gridNow } = useLiveClock(beat * GRID_REFRESH_FACTOR, live);

  const { series, pods, askMs, loading, error } = useDeploymentMetrics(
    deploymentId,
    view,
    now,
  );

  // The whole catalogue, alongside the overview. A separate hook rather than one
  // larger request: the overview is two metrics and lands immediately, and it
  // should be on screen while the other fifty are still arriving.
  const catalogue = useDeploymentCatalogue(deploymentId, view, askMs, gridNow);

  const setView = (next: View) => {
    const qs = writeView(next);
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
  };

  const { cpu, memory } = useMemo(() => split(series), [series]);

  // The axis spans what came back, not what was asked for. A view asks its tier
  // for more than the tier may hold — deliberately, so nothing is hidden — and
  // drawing the requested window instead would squeeze ten minutes of live data
  // into the left sixth of an hour-wide plot.
  const [from, to] = useMemo(
    () => covered(cpu, memory, now, askMs),
    [cpu, memory, now, askMs],
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-6">
      <MetricsHeader
        title={title}
        view={view}
        onView={setView}
        series={series}
        pods={pods.length}
        fromMs={from}
        toMs={to}
        live={live}
        onLive={() => setLive((on) => !on)}
        onRefresh={refresh}
        loading={loading}
        beatMs={beat}
      />

      {error && (
        <p className="rounded-md border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
          {error}
        </p>
      )}

      <div className="rounded-xl border border-black/10 bg-white/40 p-4 dark:border-white/10 dark:bg-zinc-900/30">
        {cpu.length + memory.length > 0 ? (
          <LineChart
            cpu={cpu}
            memory={memory}
            fromMs={from}
            toMs={to}
            // Only the historic view: the live tier is already at the finest
            // resolution the pod stores, and a retention window of buckets is
            // what needs narrowing.
            zoomable={view === "historic"}
          />
        ) : loading ? (
          <p className="py-10 text-center text-sm text-zinc-400">Loading metrics…</p>
        ) : (
          <Nothing failed={Boolean(error)} pods={pods.length} />
        )}
      </div>

      {series?.truncated && (
        <p className="text-xs text-amber-600 dark:text-amber-500">
          Only the most recently seen pods were read, so this is part of the picture.
        </p>
      )}

      {series?.warnings.map((warning) => (
        <p key={`${warning.pod}:${warning.reason}`} className="text-xs text-zinc-500">
          <span className="font-mono">{warning.pod}</span>: {warning.reason}
        </p>
      ))}

      <PodStatsTable pods={pods} now={now} />

      {catalogue.metrics.length > 0 ? (
        <MetricGrid
          metrics={catalogue.metrics}
          stepMs={parseGoDuration(catalogue.page?.step ?? "") ?? 1000}
          fromMs={catalogue.fromMs}
          toMs={catalogue.toMs}
        />
      ) : catalogue.loading ? (
        <p className="text-sm text-zinc-400">Loading the rest of the metrics…</p>
      ) : null}
    </div>
  );
}

/**
 * Why there is no chart. Three different states with three different fixes, and
 * collapsing them into one message would send the reader to the wrong one.
 */
function Nothing({ failed, pods }: { failed: boolean; pods: number }) {
  if (failed) {
    return (
      <EmptyState
        icon={Gauge}
        title="Metrics unavailable"
        body="The platform can't reach the observability service. Set OBSERVABILITY_URL to enable it."
      />
    );
  }
  if (pods === 0) {
    return (
      <EmptyState
        icon={Gauge}
        title="No pod stats for this deployment"
        body="Pod stats are off for this install, or this deployment has not reported yet. Enable orchestrator.podStats in the chart to collect them."
      />
    );
  }
  return (
    <EmptyState
      icon={Gauge}
      title="Nothing recorded in this window"
      body="The pods are reporting, but they stored no samples over this range. Try a wider one."
    />
  );
}


/**
 * The window the returned points actually cover, or the requested one when
 * nothing came back — an empty chart still needs an axis.
 */
function covered(
  cpu: PodSeries[],
  memory: PodSeries[],
  now: number,
  askMs: number,
): [number, number] {
  let first = Infinity;
  let last = -Infinity;
  for (const series of [...cpu, ...memory]) {
    const times = series.points.times;
    if (times.length === 0) continue;
    first = Math.min(first, times[0]);
    last = Math.max(last, times[times.length - 1]);
  }
  // A single point has no span; give it one so the axis is drawable.
  if (first === Infinity || last <= first) return [now - askMs, now];
  return [first, last];
}

/** Split the response into the two charts' inputs, converting as each kind
 * requires: a counter into cores, a gauge as stored. */
function split(page: StatsSeriesPage | null): { cpu: PodSeries[]; memory: PodSeries[] } {
  if (!page) return { cpu: [], memory: [] };

  const stepMs = parseGoDuration(page.step) ?? 1000;
  const cpu: PodSeries[] = [];
  const memory: PodSeries[] = [];

  for (const series of page.series) {
    if (series.name === CPU_METRIC) {
      cpu.push({ pod: series.pod, points: toCores(series, stepMs) });
    } else if (series.name === MEM_METRIC) {
      memory.push({ pod: series.pod, points: toGauge(series) });
    }
  }
  return { cpu, memory };
}
