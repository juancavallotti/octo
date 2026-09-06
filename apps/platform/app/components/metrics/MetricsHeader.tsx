"use client";

import Link from "next/link";
import { ArrowLeft, Pause, Play, RefreshCw } from "lucide-react";
import { formatStep, parseGoDuration } from "@/app/components/stats/chart/duration";
import type { StatsSeriesPage } from "@/app/model/stats";
import { VIEWS, type View } from "./range";

/**
 * The page's title bar: where you came from, which view you are in, and what
 * that view actually returned.
 *
 * The last of those is the load-bearing one. A tier holds what it holds — the
 * live tier keeps one rollup interval's worth of samples, so an install with a
 * short bucket has a short live tier — and a reader who chose "Hourly" and
 * received ten minutes should be told so rather than left to infer it from the
 * axis. The same line explains why a week is a few hundred points rather than
 * a smooth curve.
 */
export default function MetricsHeader({
  title,
  view,
  onView,
  series,
  pods,
  fromMs,
  toMs,
  live,
  onLive,
  onRefresh,
  loading,
  beatMs,
}: {
  title?: string;
  view: View;
  onView: (next: View) => void;
  series: StatsSeriesPage | null;
  pods: number;
  fromMs: number;
  toMs: number;
  live: boolean;
  onLive: () => void;
  onRefresh: () => void;
  loading: boolean;
  beatMs: number;
}) {
  return (
    <div className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <Link
          href="/platform/deployments"
          className="inline-flex items-center gap-1 text-xs text-zinc-500 hover:text-zinc-900 dark:hover:text-zinc-100"
        >
          <ArrowLeft size={12} /> Deployments
        </Link>
        <h1 className="text-lg font-semibold">{title ?? "Metrics"}</h1>
        <p className="text-xs text-zinc-500">{describe(series, pods, fromMs, toMs)}</p>
      </div>

      <div className="flex items-center gap-2">
        {VIEWS.map((preset) => (
          <button
            key={preset.key}
            type="button"
            aria-pressed={view === preset.key}
            onClick={() => onView(preset.key)}
            className={`rounded-md border px-2.5 py-1 text-xs ${
              view === preset.key
                ? "border-sky-500/40 bg-sky-500/10 text-sky-600 dark:text-sky-400"
                : "border-black/10 text-zinc-500 hover:text-zinc-900 dark:border-white/15 dark:hover:text-zinc-100"
            }`}
          >
            {preset.label}
          </button>
        ))}
        <button
          type="button"
          onClick={onLive}
          aria-pressed={live}
          aria-label={live ? "Pause automatic refresh" : "Resume automatic refresh"}
          title={
            live
              ? `Updating every ${Math.round(beatMs / 1000)}s`
              : "Paused — the window is held where you left it"
          }
          className={`inline-flex items-center gap-1 rounded-md border px-2 py-1 text-xs ${
            live
              ? "border-emerald-500/40 bg-emerald-500/10 text-emerald-600 dark:text-emerald-400"
              : "border-black/10 text-zinc-500 hover:text-zinc-900 dark:border-white/15 dark:hover:text-zinc-100"
          }`}
        >
          {live ? <Pause size={12} /> : <Play size={12} />}
          {/* Not "Live": that is the name of a view now, and a button beside it
              reading the same word would look like a second way to choose one. */}
          {live ? "Auto" : "Paused"}
        </button>
        <button
          type="button"
          onClick={onRefresh}
          aria-label="Refresh"
          className="rounded-md border border-black/10 p-1.5 text-zinc-500 hover:text-zinc-900 dark:border-white/15 dark:hover:text-zinc-100"
        >
          <RefreshCw size={14} className={loading ? "animate-spin" : undefined} />
        </button>
      </div>
    </div>
  );
}

/** Which tier answered, at what resolution, and how far it actually reached. */
function describe(
  series: StatsSeriesPage | null,
  pods: number,
  fromMs: number,
  toMs: number,
): string {
  const reporting = `${pods} ${pods === 1 ? "pod" : "pods"}`;
  if (!series) return reporting;

  const step = parseGoDuration(series.step);
  const parts = [series.tier === "live" ? "live" : "history"];
  if (step !== null) parts.push(`one point per ${formatStep(step)}`);
  if (toMs > fromMs) parts.push(`last ${formatStep(toMs - fromMs)}`);
  parts.push(reporting);
  return parts.join(" · ");
}
