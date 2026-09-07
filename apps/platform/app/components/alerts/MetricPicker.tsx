"use client";

import { useEffect, useState } from "react";
import { AppPicker } from "@/app/components/AppPicker";
import { listStatsMetrics } from "@/app/model/stats";

/**
 * A runtime metric, chosen from what the deployment is actually exporting — plus
 * the ones that are not there yet and should be.
 *
 * The second half is the point. `octo_flow_errors_total` is only created the
 * first time something fails, so the metric anybody most wants to alert on is
 * absent from the endpoint right up until the moment it would have fired. A
 * picker that offered only what has been scraped would make it impossible to
 * write that alert in advance, which is the wrong way round. See
 * https://github.com/juancavallotti/octo/issues/441.
 *
 * So known runtime series are always offered, marked when nothing has reported
 * them yet, and anything else the deployment exports is offered beside them.
 */

/**
 * Series the runtime defines, whether or not one has been observed.
 *
 * Short and deliberately not a copy of the whole registry: these are the ones
 * worth alerting on, and a list that tried to mirror everything would go stale
 * without anybody noticing.
 */
const KNOWN: { name: string; hint: string }[] = [
  {
    name: "octo_flow_messages_total",
    hint: "Messages a flow finished with, by outcome — including failed. Always present.",
  },
  {
    name: "octo_flow_errors_total",
    hint: "Unhandled failures, by the block they came from. Only exists once something has failed.",
  },
  {
    name: "octo_flow_duration_seconds",
    hint: "How long a flow took, as a histogram.",
  },
  {
    name: "octo_flow_in_flight",
    hint: "Messages a flow is processing right now.",
  },
];

interface MetricChoice {
  name: string;
  hint?: string;
  /** Whether the deployment has actually reported it. */
  exported: boolean;
}

export function MetricPicker({
  deploymentId,
  value,
  onChange,
}: {
  deploymentId: string;
  value: string;
  onChange: (metric: string) => void;
}) {
  const [exported, setExported] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    if (!deploymentId) return;
    let stopped = false;
    const load = async () => {
      setLoading(true);
      try {
        const page = await listStatsMetrics(deploymentId, {});
        if (stopped) return;
        setExported(page.items.map((m) => m.name));
        setProblem(null);
      } catch (e) {
        // Shown rather than swallowed. An unreachable stats API and a deployment
        // exporting nothing look identical in an empty list, and they want quite
        // different things done about them.
        if (!stopped) setProblem((e as Error).message);
      } finally {
        if (!stopped) setLoading(false);
      }
    };
    void load();
    return () => {
      stopped = true;
    };
  }, [deploymentId]);

  const items = merge(exported, value);
  const selected = items.find((m) => m.name === value) ?? null;

  return (
    <div className="flex flex-col gap-1">
      <AppPicker<MetricChoice>
        items={items}
        selected={selected}
        onSelect={(metric) => onChange(metric.name)}
        toKey={(metric) => metric.name}
        toText={(metric) => `${metric.name} ${metric.hint ?? ""}`}
        renderValue={(metric) => (
          <span className="font-mono text-xs">{metric.name}</span>
        )}
        renderRow={(metric) => (
          <span className="flex w-full flex-col gap-0.5">
            <span className="flex items-center justify-between gap-3">
              <span className="truncate font-mono text-xs">{metric.name}</span>
              {!metric.exported && (
                <span className="shrink-0 text-[11px] text-amber-600 dark:text-amber-400">
                  not reported yet
                </span>
              )}
            </span>
            {metric.hint && (
              <span className="text-[11px] text-zinc-500">{metric.hint}</span>
            )}
          </span>
        )}
        label="Runtime metric"
        placeholder="Search metrics…"
        loading={loading}
        empty={
          <p className="px-3 py-6 text-center text-xs text-zinc-500">
            {deploymentId
              ? "This deployment is exporting nothing. Pod stats have to be turned on in the chart."
              : "Choose the app first — metrics are exported per deployment."}
          </p>
        }
      />
      {problem && (
        <p className="text-xs text-red-600 dark:text-red-400">
          Could not read this deployment&rsquo;s metrics: {problem}
        </p>
      )}
      {!deploymentId && (
        <p className="text-xs text-amber-600 dark:text-amber-400">
          Pod stats are stored per deployment. Choose the app in step 1 and the
          list fills in.
        </p>
      )}
    </div>
  );
}

/**
 * The known series and the exported ones as one list, without duplicates, with
 * whatever is currently set kept even if neither knows about it.
 */
function merge(exported: string[], current: string): MetricChoice[] {
  const seen = new Set<string>();
  const out: MetricChoice[] = [];
  const add = (name: string, hint?: string) => {
    if (!name || seen.has(name)) return;
    seen.add(name);
    out.push({ name, hint, exported: exported.includes(name) });
  };

  for (const known of KNOWN) add(known.name, known.hint);
  for (const name of [...exported].sort()) add(name);
  add(current);
  return out;
}
