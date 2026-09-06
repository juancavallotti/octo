"use client";

import Link from "next/link";
import { bytes } from "@/app/components/stats/Stat";
import Sparkline from "@/app/components/stats/chart/Sparkline";
import { formatCores, latest } from "@/app/components/stats/chart/metrics";
import type { SparkData } from "./useDeploymentStats";

/**
 * The last five minutes of a deployment's CPU and memory, on its card.
 *
 * A shape and two numbers, linked to the page that explains them. The shape
 * answers the question somebody scanning this page is actually asking — is
 * anything moving — and the numbers keep it honest, because a sparkline with no
 * axis makes a two-percent wobble and a doubling look identical.
 *
 * Absent rather than empty when there is nothing to show. Pod stats are off by
 * default, so most installs will never have this data, and a row of dashes on
 * every card would advertise a missing feature to everyone who did not ask for
 * it.
 */
export default function DeploymentSpark({
  deploymentId,
  data,
}: {
  deploymentId: string;
  data: SparkData | undefined;
}) {
  if (!data) return null;

  const cores = latest(data.cpu);
  const memory = latest(data.memory);
  if (cores === null && memory === null) return null;

  return (
    <Link
      href={metricsHref(deploymentId)}
      title="Metrics for the last five minutes"
      className="mt-3 flex items-center gap-3 rounded-lg border border-black/5 px-2 py-1.5 transition-colors hover:bg-black/[0.03] dark:border-white/10 dark:hover:bg-white/[0.05]"
    >
      <Sparkline
        cpu={data.cpu}
        memory={data.memory}
        label={`CPU ${formatCores(cores)} cores, memory ${
          memory === null ? "unknown" : bytes(memory)
        }, over the last five minutes`}
      />
      <span className="flex flex-col text-xs leading-tight">
        <span className="tabular-nums text-sky-600 dark:text-sky-400">
          {formatCores(cores)} <span className="text-zinc-400">cores</span>
        </span>
        <span className="tabular-nums text-violet-600 dark:text-violet-400">
          {memory === null ? "—" : bytes(memory)}
        </span>
      </span>
    </Link>
  );
}

/**
 * This deployment's metrics page. Keyed on the deployment id rather than on app
 * name and version the way logsHref is, because the stats index is
 * deployment-first: `octo:stats:v0:{deployment}:` is what finds a pod's rows, and
 * there is no way to ask for them by app.
 */
export function metricsHref(deploymentId: string): string {
  return `/platform/metrics/${encodeURIComponent(deploymentId)}`;
}
