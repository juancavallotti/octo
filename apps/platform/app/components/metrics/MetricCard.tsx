"use client";

import MiniChart, { type MiniSeries } from "@/app/components/stats/chart/MiniChart";
import { latest, toCores, toGauge } from "@/app/components/stats/chart/metrics";
import type { StatsSeries } from "@/app/model/stats";
import type { CataloguedMetric } from "./useDeploymentCatalogue";
import { unitFor } from "./units";

/**
 * One metric in the grid: its name, what it currently reads, and its shape.
 *
 * The current value is the headline rather than the chart, because most of these
 * fifty metrics are answered by a number — how many goroutines, how many open
 * file descriptors — and only some of them by a trend. The chart is what tells
 * you which kind you are looking at.
 */
export default function MetricCard({
  entry,
  stepMs,
  fromMs,
  toMs,
}: {
  entry: CataloguedMetric;
  stepMs: number;
  fromMs: number;
  toMs: number;
}) {
  const unit = unitFor(entry.name, entry.metric.kind);

  // A metric whose value is always 1 carries its content in its labels. A flat
  // line at 1 says nothing; the version string it is holding says everything.
  if (unit.unit === "info") {
    return <InfoCard entry={entry} />;
  }

  const columns = entry.series.map((series) => ({
    ...points(series, unit.rate, stepMs),
    // Pod and label set together: one metric can carry a hundred series of one
    // pod, so the pod name alone is not an identity.
    key: `${series.pod}:${labelKey(series)}`,
  }));

  const reading = current(columns);
  const count = entry.series.length;
  const steady = isSteady(columns);

  return (
    <div className="flex flex-col rounded-xl border border-black/10 bg-white/40 p-3 dark:border-white/10 dark:bg-zinc-900/30">
      <div className="flex items-baseline justify-between gap-2">
        <p className="min-w-0 truncate font-mono text-xs" title={entry.name}>
          {entry.name}
        </p>
        {/* Suppressed when steady: the card states the same number in the
            middle, and saying it twice reads as two different facts. */}
        {!steady && (
          <p className="shrink-0 tabular-nums text-xs text-sky-600 dark:text-sky-400">
            {reading === null ? "—" : unit.format(reading)}
          </p>
        )}
      </div>
      <p className="mt-0.5 text-[10px] text-zinc-400">
        {entry.metric.kind}
        {unit.label && ` · ${unit.label}`}
        {count > 1 && ` · ${count} series`}
      </p>

      {columns.length === 0 ? (
        <p className="flex h-24 items-center justify-center text-[11px] text-zinc-400">
          nothing recorded
        </p>
      ) : steady ? (
        // A line that never moved has no shape to show, and drawing one costs a
        // reader a second working out that the flat line is the whole story.
        // Some of these are constants by nature — a file-descriptor limit, a
        // GOGC setting, a process start time — and charting a start time from
        // any sensible axis produces a scale nobody can read.
        <p className="flex h-24 flex-col items-center justify-center gap-1">
          <span className="tabular-nums text-lg">
            {reading === null ? "—" : unit.format(reading)}
          </span>
          <span className="text-[10px] text-zinc-400">unchanged over this window</span>
        </p>
      ) : (
        <MiniChart
          series={columns}
          fromMs={fromMs}
          toMs={toMs}
          format={unit.format}
          labelFormat={clock}
          binary={unit.unit === "bytes"}
          anchorZero={unit.anchorZero ?? true}
        />
      )}
    </div>
  );
}

/** A metric that is a fact rather than a measurement. */
function InfoCard({ entry }: { entry: CataloguedMetric }) {
  const labels = entry.metric.series.flatMap((s) => Object.entries(s.labels));

  return (
    <div className="flex flex-col rounded-xl border border-black/10 bg-white/40 p-3 dark:border-white/10 dark:bg-zinc-900/30">
      <p className="truncate font-mono text-xs" title={entry.name}>
        {entry.name}
      </p>
      <p className="mt-0.5 text-[10px] text-zinc-400">
        constant · reported through its labels
      </p>
      <dl className="mt-2 flex flex-col gap-1">
        {labels.map(([key, value]) => (
          <div key={key} className="flex min-w-0 items-baseline gap-2 text-[11px]">
            <dt className="shrink-0 text-zinc-400">{key}</dt>
            <dd className="min-w-0 truncate font-mono" title={value}>
              {value}
            </dd>
          </div>
        ))}
        {labels.length === 0 && (
          <span className="text-[11px] text-zinc-400">no labels</span>
        )}
      </dl>
    </div>
  );
}

/** A counter becomes a rate; a gauge is read as stored. */
function points(series: StatsSeries, rate: boolean, stepMs: number): Omit<MiniSeries, "key"> {
  const converted = rate ? toCores(series, stepMs) : toGauge(series);
  return { times: converted.times, values: converted.values };
}

/** The latest reading across every series, summed when there is more than one —
 * eight goroutine counts from eight pods is one deployment's goroutines. */
function current(columns: MiniSeries[]): number | null {
  let total: number | null = null;
  for (const column of columns) {
    const value = latest({ times: column.times, values: column.values });
    if (value === null) continue;
    total = (total ?? 0) + value;
  }
  return total;
}

/**
 * Whether nothing moved. Every finite reading of every series equal to that
 * series' first — a gap is not a change, since nothing was measured.
 */
function isSteady(columns: MiniSeries[]): boolean {
  let sawAny = false;
  for (const column of columns) {
    let first: number | null = null;
    for (const value of column.values) {
      if (value === null || !Number.isFinite(value)) continue;
      sawAny = true;
      if (first === null) first = value;
      else if (value !== first) return false;
    }
  }
  return sawAny;
}

/** A stable identity for one label set. */
function labelKey(series: StatsSeries): string {
  return Object.entries(series.labels)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([k, v]) => `${k}=${v}`)
    .join(",");
}

/** The moment under the cursor, to the second: these charts are seconds wide. */
function clock(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}
