"use client";

import type { CataloguedMetric } from "./useDeploymentCatalogue";
import { byFamily } from "./family";
import MetricCard from "./MetricCard";

/**
 * Everything the pod exposes, in a grid, under family headings.
 *
 * Fifty names is too many to pick through and few enough to show, so this shows
 * all of them and lets the eye do the picking — which is what the sections are
 * for. A reader who came here because something looked wrong on the overview
 * chart is scanning for a second shape that moved at the same moment, and that
 * scan only works if the charts are all on screen together.
 */
export default function MetricGrid({
  metrics,
  stepMs,
  fromMs,
  toMs,
}: {
  metrics: CataloguedMetric[];
  stepMs: number;
  fromMs: number;
  toMs: number;
}) {
  if (metrics.length === 0) return null;

  return (
    <div className="flex flex-col gap-6">
      {byFamily(metrics).map(({ family, items }) => (
        <section key={family.key}>
          <h2 className="text-sm font-semibold">{family.label}</h2>
          <p className="mb-2 text-xs text-zinc-500">
            {family.blurb} <span className="text-zinc-400">{items.length} metrics</span>
          </p>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4">
            {items.map((entry) => (
              <MetricCard
                key={entry.name}
                entry={entry}
                stepMs={stepMs}
                fromMs={fromMs}
                toMs={toMs}
              />
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}
