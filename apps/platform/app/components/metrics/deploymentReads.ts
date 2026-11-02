import type { ActionResult } from "@octo/http";
import type {
  StatsMetric,
  StatsMetricsPage,
  StatsPod,
  StatsPodsPage,
  StatsSeries,
  StatsSeriesPage,
  StatsSeriesQuery,
  StatsWarning,
} from "@/app/model/stats";
import { CPU_METRIC, MEM_METRIC } from "@/app/components/stats/chart/metrics";
import { planBatches } from "./batches";
import { reachFor, viewPreset, windowFor, type View } from "./range";

/**
 * What a deployment's metrics page reads on each tick, composed in one place so
 * the server action can run it whole: the browser sends actions one at a time,
 * and asking for the pods, then the series, then the catalogue, then each batch
 * from there was a round trip apiece.
 *
 * The reads are passed in rather than imported, which keeps this free of the
 * server-only client and lets a test hand it fakes.
 *
 * Both readers start from the pods, because the pods say how far back a view's
 * tier reaches. Knowing that before the series is asked for is what stops the
 * first tick asking with a guess and the second asking again with the answer.
 */

export interface StatsReads {
  getPods(deploymentId: string): Promise<ActionResult<StatsPodsPage>>;
  getMetrics(deploymentId: string): Promise<ActionResult<StatsMetricsPage>>;
  getSeries(
    deploymentId: string,
    query: StatsSeriesQuery,
  ): Promise<ActionResult<StatsSeriesPage>>;
}

/** The overview chart's data, and the pods that explain an empty one. */
export interface Overview {
  /** Null when the series read failed; the pods may still have answered. */
  series: StatsSeriesPage | null;
  /** Null when the pod read failed. */
  pods: StatsPod[] | null;
  /** The first failure, or null. Half an answer is still drawn. */
  error: string | null;
}

export async function readOverview(
  reads: StatsReads,
  deploymentId: string,
  view: View,
  nowMs: number,
): Promise<Overview> {
  const pods = await reads.getPods(deploymentId);
  const askMs = reachFor(view, pods.ok ? pods.data.items : []);
  const window = windowFor(view, nowMs, askMs);

  const series = await reads.getSeries(deploymentId, {
    metrics: [CPU_METRIC, MEM_METRIC],
    // The view names its tier. Never auto: a window longer than the live tier
    // reaches would be answered entirely from buckets, which is a hundred and
    // twenty points becoming two without saying so.
    tier: viewPreset(view).tier,
    from: window.from,
    to: window.to,
    // The service clamps to 5000; 1500 is more points than the widest chart has
    // pixels, and asking for the cap would mostly move bytes.
    limit: 1500,
  });

  return {
    series: series.ok ? series.data : null,
    pods: pods.ok ? pods.data.items : null,
    error: !series.ok ? series.error : !pods.ok ? pods.error : null,
  };
}

export interface CataloguedMetric {
  name: string;
  metric: StatsMetric;
  series: StatsSeries[];
}

/** Every metric a deployment exposes, and the points behind each. */
export interface CataloguePage {
  metrics: CataloguedMetric[];
  /** The window these points cover. */
  fromMs: number;
  toMs: number;
  /** The resolved tier and step, from the first batch. */
  page: StatsSeriesPage | null;
  warnings: StatsWarning[];
  truncated: boolean;
}

export async function readCatalogue(
  reads: StatsReads,
  deploymentId: string,
  view: View,
  nowMs: number,
): Promise<ActionResult<CataloguePage>> {
  const pods = await reads.getPods(deploymentId);
  const window = windowFor(
    view,
    nowMs,
    reachFor(view, pods.ok ? pods.data.items : []),
  );

  const catalogue = await reads.getMetrics(deploymentId);
  if (!catalogue.ok) return catalogue;

  // The catalogue says how many series each name resolves to, which is what
  // lets the batches be packed to a size the service will answer.
  const pages = await Promise.all(
    planBatches(catalogue.data.items).map((names) =>
      reads.getSeries(deploymentId, {
        metrics: names,
        tier: viewPreset(view).tier,
        from: window.from,
        to: window.to,
        limit: 1500,
      }),
    ),
  );
  const failed = pages.find((p) => !p.ok);
  if (failed && !failed.ok) return failed;
  const answered = pages.flatMap((p) => (p.ok ? [p.data] : []));

  // One bucket per metric name, so a metric the catalogue knows but that stored
  // no rows still gets a card saying so, rather than vanishing.
  const byName = new Map<string, StatsSeries[]>(
    catalogue.data.items.map((m) => [m.name, []]),
  );
  for (const page of answered) {
    for (const series of page.series) byName.get(series.name)?.push(series);
  }

  return {
    ok: true,
    data: {
      metrics: catalogue.data.items.map((metric) => ({
        name: metric.name,
        metric,
        series: byName.get(metric.name) ?? [],
      })),
      page: answered[0] ?? null,
      fromMs: Date.parse(window.from),
      toMs: Date.parse(window.to),
      warnings: dedupe([
        ...catalogue.data.warnings,
        ...answered.flatMap((page) => page.warnings),
      ]),
      truncated:
        catalogue.data.truncated || answered.some((page) => page.truncated),
    },
  };
}

/** The same pod and reason from several batches is one warning, not five. */
function dedupe(warnings: StatsWarning[]): StatsWarning[] {
  const seen = new Map<string, StatsWarning>();
  for (const warning of warnings) seen.set(`${warning.pod}:${warning.reason}`, warning);
  return [...seen.values()];
}
