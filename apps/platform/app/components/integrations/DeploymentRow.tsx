"use client";

import {
  AlertTriangle,
  Clock,
  GitBranch,
  RotateCcw,
  Tag,
  Trash2,
  Waypoints,
} from "lucide-react";
import type { Deployment } from "@/app/model/orchestrator";
import ReplicaStepper from "./ReplicaStepper";
import { relativeAge } from "@/app/lib/relativeAge";
import { totalRestarts } from "./podStats";
import { AddressLine, StatusBadge } from "./DeploymentRowParts";
import PodList from "./PodList";

/**
 * One row in the deployments list, laid out as a small card: a header line with
 * the status badge, short id, ready/desired replicas, restarts and age plus the
 * Undeploy action, then clearly-labelled External/Internal address lines and the
 * failure reason when failed. Split out of DeploymentsSection to keep that
 * component focused on data/actions.
 */

export default function DeploymentRow({
  deployment: d,
  busy,
  onScale,
  onOpenRollout,
  onUndeploy,
  onOpenLogs,
}: {
  deployment: Deployment;
  busy: boolean;
  onScale: (d: Deployment, replicas: number) => void;
  /** Open the rollout dialog to change this deployment's version and/or env. */
  onOpenRollout?: (d: Deployment) => void;
  onUndeploy: (d: Deployment) => void;
  /** Open the dockable log panel tailing a specific pod of this deployment. */
  onOpenLogs?: (d: Deployment, podName: string) => void;
}) {
  const age = relativeAge(d.createdAt);
  const restarts = totalRestarts(d);
  const desired = d.desiredReplicas || d.replicas;
  const pods = d.pods ?? [];
  // Pods are collapsed by default to keep the row compact; expand to tail logs.

  return (
    <li
      className="rounded-lg border border-zinc-200 bg-white/40 px-3 py-2 dark:border-zinc-800 dark:bg-zinc-900/30"
      title={d.id}
    >
      <div className="flex items-center gap-2.5">
        <StatusBadge status={d.status} />
        <span className="font-mono text-xs text-zinc-500">
          {d.id.slice(0, 8)}
        </span>
        {d.tag && (
          <span
            className="inline-flex items-center gap-1 rounded-full bg-sky-500/15 px-2 py-0.5 text-xs font-medium text-sky-600 dark:text-sky-400"
            title={`Version ${d.tag}`}
          >
            <Tag size={10} />
            {d.tag}
          </span>
        )}
        {d.tracing && (
          <span
            className="inline-flex items-center gap-1 rounded-full bg-violet-500/15 px-2 py-0.5 text-xs font-medium text-violet-600 dark:text-violet-400"
            title="Tracing is on for this deployment — every flow, block and model call is recorded"
          >
            <Waypoints size={10} />
            Traced
          </span>
        )}
        <ReplicaStepper
          desired={desired}
          busy={busy}
          onScale={(n) => onScale(d, n)}
        />
        <span className="text-xs text-zinc-500">
          <span className="font-medium text-zinc-700 dark:text-zinc-200">
            {d.readyReplicas}
          </span>{" "}
          ready
        </span>
        {restarts > 0 && (
          <span
            className="inline-flex items-center gap-0.5 text-xs text-amber-600 dark:text-amber-400"
            title="Container restarts"
          >
            <RotateCcw size={11} />
            {restarts}
          </span>
        )}
        {age && (
          <span
            className="inline-flex items-center gap-0.5 text-xs text-zinc-400"
            title="Age"
          >
            <Clock size={11} />
            {age}
          </span>
        )}
        <div className="ml-auto flex items-center gap-1">
          {onOpenRollout && (
            <button
              type="button"
              disabled={busy}
              onClick={() => onOpenRollout(d)}
              title="Change version or environment"
              aria-label="Roll out"
              className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-600 disabled:opacity-50 dark:hover:bg-white/[0.08] dark:hover:text-zinc-300"
            >
              <GitBranch size={14} />
            </button>
          )}
          <button
            type="button"
            onClick={() => onUndeploy(d)}
            disabled={busy}
            aria-label="Undeploy"
            className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
          >
            <Trash2 size={14} />
          </button>
        </div>
      </div>

      {d.reason && (
        <div className="mt-1.5 flex items-start gap-1 text-xs text-red-500">
          <AlertTriangle size={12} className="mt-0.5 shrink-0" />
          <span className="break-words">{d.reason}</span>
        </div>
      )}

      {(d.externalUrl || d.internalUrl) && (
        <div className="mt-2 space-y-1 border-t border-zinc-100 pt-2 dark:border-zinc-800/70">
          {d.externalUrl && (
            <AddressLine
              label="External"
              value={d.externalUrl}
              href={d.externalUrl}
            />
          )}
          {d.internalUrl && (
            <AddressLine label="Internal" value={d.internalUrl} />
          )}
        </div>
      )}

      {onOpenLogs && pods.length > 0 && (
        <PodList pods={pods} onOpenLogs={(pod) => onOpenLogs(d, pod)} />
      )}
    </li>
  );
}
