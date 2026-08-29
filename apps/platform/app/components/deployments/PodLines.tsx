"use client";

import { RotateCcw, ScrollText } from "lucide-react";
import type { PodStatus } from "@/app/model/orchestrator";

/**
 * A deployment's pods on the deployments page: one line each, with its phase,
 * readiness, restarts and a button that tails its logs.
 *
 * Always expanded, unlike the integrations panel's collapsible PodList. A row
 * there sits inside a folder tree beside a dozen siblings and has to stay short;
 * here the page *is* the deployments, and per-pod state is what it exists to show.
 */

// Pod phases come straight from Kubernetes; colour the healthy/terminal ones.
const PHASE_DOT: Record<string, string> = {
  Running: "bg-emerald-500",
  Pending: "bg-amber-500",
  Succeeded: "bg-sky-500",
  Failed: "bg-red-500",
  Unknown: "bg-zinc-400",
};

export default function PodLines({
  pods,
  onOpenLogs,
}: {
  pods: PodStatus[];
  /** Absent when the page has no log panel to open. */
  onOpenLogs?: (podName: string) => void;
}) {
  if (pods.length === 0) {
    return <p className="text-xs text-zinc-400">No pods reported.</p>;
  }
  return (
    <ul className="space-y-1.5">
      {pods.map((pod) => (
        <li key={pod.name} className="flex items-center gap-2 text-xs">
          <span
            className={`h-1.5 w-1.5 shrink-0 rounded-full ${PHASE_DOT[pod.phase] ?? "bg-zinc-400"}`}
            title={pod.phase}
          />
          <span
            className="min-w-0 flex-1 truncate font-mono text-zinc-600 dark:text-zinc-300"
            title={pod.name}
          >
            {pod.name}
          </span>
          <span className="shrink-0 text-zinc-400">{pod.phase}</span>
          <span
            className={`shrink-0 ${pod.ready ? "text-emerald-600 dark:text-emerald-400" : "text-zinc-400"}`}
          >
            {pod.ready ? "ready" : "not ready"}
          </span>
          {pod.restarts > 0 && (
            <span
              className="inline-flex shrink-0 items-center gap-0.5 text-amber-600 dark:text-amber-400"
              title="Container restarts"
            >
              <RotateCcw size={10} />
              {pod.restarts}
            </span>
          )}
          {onOpenLogs && (
            <button
              type="button"
              onClick={() => onOpenLogs(pod.name)}
              aria-label={`View logs for ${pod.name}`}
              title="Tail this pod's logs"
              className="shrink-0 rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-600 dark:hover:bg-white/[0.08] dark:hover:text-zinc-300"
            >
              <ScrollText size={13} />
            </button>
          )}
        </li>
      ))}
    </ul>
  );
}
