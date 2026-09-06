"use client";

import { useEffect, useState } from "react";
import {
  listStatsMetrics,
  readStatsSeries,
  type StatsMetric,
  type StatsSeries,
  type StatsSeriesPage,
  type StatsWarning,
} from "@/app/model/stats";
import { windowFor, type RangeKey } from "./range";
import { planBatches } from "./batches";

/**
 * Every metric a deployment exposes, and the points behind all of them.
 *
 * The catalogue comes first and is what makes the rest bounded: it names the
 * metrics and says how many series each will resolve to, so the series requests
 * can be packed to a size the service will answer rather than guessed at.
 *
 * All of it is fetched at once — three requests and a few hundred kilobytes for
 * a real deployment's fifty names — because the page exists to be scanned. A
 * chart that fills in when it scrolls into view is a chart nobody compares
 * against the one above it.
 */

export interface CataloguedMetric {
  name: string;
  metric: StatsMetric;
  series: StatsSeries[];
}

export interface Catalogue {
  metrics: CataloguedMetric[];
  /** The resolved tier and step, from whichever request answered first. */
  page: StatsSeriesPage | null;
  warnings: StatsWarning[];
  truncated: boolean;
  loading: boolean;
  error: string | null;
}

export function useDeploymentCatalogue(
  deploymentId: string,
  range: RangeKey,
  now: number,
): Catalogue {
  const [state, setState] = useState<Omit<Catalogue, "loading">>({
    metrics: [],
    page: null,
    warnings: [],
    truncated: false,
    error: null,
  });
  const [loaded, setLoaded] = useState<string | null>(null);

  const wanted = `${deploymentId} ${range} ${now}`;

  useEffect(() => {
    let cancelled = false;
    const window = windowFor(range, now);

    void (async () => {
      try {
        const catalogue = await listStatsMetrics(deploymentId);
        if (cancelled) return;

        const pages = await Promise.all(
          planBatches(catalogue.items).map((names) =>
            readStatsSeries(deploymentId, {
              metrics: names,
              tier: "auto",
              from: window.from,
              to: window.to,
              limit: 1500,
            }),
          ),
        );
        if (cancelled) return;

        // One bucket per metric name, so a metric the catalogue knows but that
        // stored no rows still gets a card saying so, rather than vanishing.
        const byName = new Map<string, StatsSeries[]>(
          catalogue.items.map((m) => [m.name, []]),
        );
        for (const page of pages) {
          for (const series of page.series) byName.get(series.name)?.push(series);
        }

        setState({
          metrics: catalogue.items.map((metric) => ({
            name: metric.name,
            metric,
            series: byName.get(metric.name) ?? [],
          })),
          page: pages[0] ?? null,
          warnings: dedupe([
            ...catalogue.warnings,
            ...pages.flatMap((page) => page.warnings),
          ]),
          truncated: catalogue.truncated || pages.some((page) => page.truncated),
          error: null,
        });
      } catch (err) {
        if (cancelled) return;
        // The last good catalogue stays on screen; only the error is new.
        setState((prev) => ({
          ...prev,
          error: err instanceof Error ? err.message : String(err),
        }));
      } finally {
        if (!cancelled) setLoaded(wanted);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [deploymentId, range, now, wanted]);

  return { ...state, loading: loaded !== wanted };
}

/** The same pod and reason from several batches is one warning, not five. */
function dedupe(warnings: StatsWarning[]): StatsWarning[] {
  const seen = new Map<string, StatsWarning>();
  for (const warning of warnings) seen.set(`${warning.pod}:${warning.reason}`, warning);
  return [...seen.values()];
}
