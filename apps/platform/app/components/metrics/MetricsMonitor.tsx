"use client";

import { useMemo, useState } from "react";
import Link from "next/link";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { ArrowLeft, Gauge, Pause, Play, RefreshCw } from "lucide-react";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import LineChart, { type PodSeries } from "@/app/components/stats/chart/LineChart";
import { formatStep, parseGoDuration } from "@/app/components/stats/chart/duration";
import {
  CPU_METRIC,
  MEM_METRIC,
  toCores,
  toGauge,
} from "@/app/components/stats/chart/metrics";
import type { StatsSeriesPage } from "@/app/model/stats";
import MetricGrid from "./MetricGrid";
import PodStatsTable from "./PodStatsTable";
import {
  GRID_REFRESH_FACTOR,
  RANGE_PRESETS,
  readRange,
  refreshMs,
  spanMs,
  writeRange,
  type RangeKey,
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
  const range = readRange(new URLSearchParams(params.toString()));

  // Live by default. The window is a moving one, so the page should behave like
  // the thing it is showing.
  const [live, setLive] = useState(true);
  const beat = refreshMs(range);
  const { now, refresh } = useLiveClock(beat, live);
  const { now: gridNow } = useLiveClock(beat * GRID_REFRESH_FACTOR, live);

  const { series, pods, loading, error } = useDeploymentMetrics(deploymentId, range, now);

  // The whole catalogue, alongside the overview. A separate hook rather than one
  // larger request: the overview is two metrics and lands immediately, and it
  // should be on screen while the other fifty are still arriving.
  const catalogue = useDeploymentCatalogue(deploymentId, range, gridNow);

  const setRange = (next: RangeKey) => {
    const qs = writeRange(next);
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
  };

  const { cpu, memory } = useMemo(() => split(series), [series]);
  const to = series ? Date.parse(series.to) : now;
  const from = series ? Date.parse(series.from) : now - spanMs(range);

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <Link
            href="/platform/deployments"
            className="inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
          >
            <ArrowLeft size={12} /> Deployments
          </Link>
          <h1 className="text-lg font-semibold">{title ?? "Metrics"}</h1>
          <p className="text-xs text-zinc-500">{describe(series, pods.length)}</p>
        </div>

        <div className="flex items-center gap-2">
          {RANGE_PRESETS.map((preset) => (
            <button
              key={preset.key}
              type="button"
              aria-pressed={range === preset.key}
              onClick={() => setRange(preset.key)}
              className={`rounded-md border px-2 py-1 text-xs ${
                range === preset.key
                  ? "border-sky-500/40 bg-sky-500/10 text-sky-600 dark:text-sky-400"
                  : "border-black/10 text-zinc-500 hover:text-zinc-900 dark:border-white/15 dark:hover:text-zinc-100"
              }`}
            >
              {preset.label}
            </button>
          ))}
          <button
            type="button"
            onClick={() => setLive((on) => !on)}
            aria-pressed={live}
            aria-label={live ? "Pause live updates" : "Resume live updates"}
            title={
              live
                ? `Updating every ${Math.round(beat / 1000)}s`
                : "Paused — the window is held where you left it"
            }
            className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs ${
              live
                ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
                : "border-black/10 text-zinc-500 hover:text-zinc-900 dark:border-white/15 dark:hover:text-zinc-100"
            }`}
          >
            {live ? <Pause size={12} /> : <Play size={12} />}
            {live ? "Live" : "Paused"}
          </button>
          <button
            type="button"
            onClick={refresh}
            aria-label="Refresh"
            className="rounded-md border border-black/10 p-1.5 text-zinc-500 hover:text-zinc-900 dark:border-white/15 dark:hover:text-zinc-100"
          >
            <RefreshCw size={14} className={loading ? "animate-spin" : undefined} />
          </button>
        </div>
      </div>

      {error && (
        <p className="rounded-md border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
          {error}
        </p>
      )}

      <div className="rounded-xl border border-black/10 bg-white/40 p-4 dark:border-white/10 dark:bg-zinc-900/30">
        {cpu.length + memory.length > 0 ? (
          <LineChart cpu={cpu} memory={memory} fromMs={from} toMs={to} />
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
        body="The platform can't reach the telemetry service. Set LOGS_URL to enable it."
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

/** The line under the title: which tier answered, at what resolution. */
function describe(series: StatsSeriesPage | null, pods: number): string {
  const reporting = `${pods} ${pods === 1 ? "pod" : "pods"}`;
  if (!series) return reporting;

  const step = parseGoDuration(series.step);
  const tier = series.tier === "live" ? "live" : "history";
  return step === null
    ? `${tier} · ${reporting}`
    : `${tier} · one point per ${formatStep(step)} · ${reporting}`;
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
