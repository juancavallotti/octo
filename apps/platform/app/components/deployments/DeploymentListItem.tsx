"use client";

import Link from "next/link";
import {
  AlertTriangle,
  Clock,
  ExternalLink,
  GitBranch,
  Pencil,
  ScrollText,
  SlidersHorizontal,
} from "lucide-react";
import type { DeploymentStatus } from "@/app/model/orchestrator";
import type { DeployedTile } from "@/app/(session)/platform/DashboardTiles";
import ReplicaStepper from "@/app/components/integrations/ReplicaStepper";
import { DeploymentPills } from "@/app/components/integrations/DeploymentRowParts";
import { relativeAge } from "@/app/lib/relativeAge";
import PodLines from "./PodLines";

/**
 * One deployment in the deployments page's list (as opposed to the dashboard's
 * tile grid). Unlike the read-only tile, this row shows the live per-pod state and
 * carries the management actions the dedicated page is for: scale it, roll it over
 * to another version (which is also where tracing is turned on and off), tail one
 * pod's logs, or open the integration behind it. Every one of those is delegated
 * to the parent, which performs the call and refreshes — mirroring the in-context
 * DeploymentRow.
 *
 * The version and tracing pills sit under the header for the same reason they do
 * there: the header is a fixed set of controls, and threading variable-width
 * labels through it moved the numbers and actions around.
 */

const STATUS_STYLES: Record<DeploymentStatus, string> = {
  running: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  pending: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  failed: "bg-red-500/15 text-red-600 dark:text-red-400",
};

/** Strip the scheme so an address reads as a bare host[:port]/path. */
function bareHost(url: string): string {
  return url.replace(/^https?:\/\//, "");
}

/** Logs filtered to this deployment's app (and version, when tagged). */
function logsHref(d: DeployedTile): string {
  const p = new URLSearchParams();
  if (d.name) p.set("appName", d.name);
  if (d.tag) p.set("appVersion", d.tag);
  const qs = p.toString();
  return qs ? `/platform/logs?${qs}` : "/platform/logs";
}

export default function DeploymentListItem({
  deployment: d,
  busy,
  onScale,
  onOpenRollout,
  onOpenLogs,
}: {
  deployment: DeployedTile;
  busy: boolean;
  onScale: (d: DeployedTile, replicas: number) => void;
  /** Open the rollout dialog: change version, env, or tracing. */
  onOpenRollout?: (d: DeployedTile) => void;
  /** Open the dockable log panel tailing one pod of this deployment. */
  onOpenLogs?: (d: DeployedTile, podName: string) => void;
}) {
  const age = relativeAge(d.createdAt);
  const desired = d.desiredReplicas || d.replicas;
  const pods = d.pods ?? [];

  return (
    <li
      className="rounded-xl border border-black/10 bg-white/40 p-4 dark:border-white/10 dark:bg-zinc-900/30"
      title={d.id}
    >
      <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
        <span
          className={`rounded-full px-2 py-0.5 text-xs font-medium capitalize ${
            STATUS_STYLES[d.status] ?? "bg-zinc-500/15 text-zinc-500"
          }`}
        >
          {d.status}
        </span>
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">
          {d.integrationName}
        </h3>

        <ReplicaStepper
          desired={desired}
          busy={busy}
          onScale={(n) => onScale(d, n)}
        />
        <span className="text-xs text-zinc-500">
          <span className="font-medium text-zinc-700 dark:text-zinc-200">
            {d.readyReplicas}
          </span>
          /{desired} ready
        </span>
        {age && (
          <span
            className="inline-flex items-center gap-0.5 text-xs text-zinc-400"
            title="Age"
          >
            <Clock size={11} />
            {age}
          </span>
        )}

        <div className="flex items-center gap-2">
          {onOpenRollout && (
            <button
              type="button"
              disabled={busy}
              onClick={() => onOpenRollout(d)}
              title="Change version, environment or tracing"
              className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-2.5 py-1 text-xs font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] hover:text-zinc-900 disabled:opacity-50 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06] dark:hover:text-zinc-100"
            >
              <GitBranch size={13} />
              Roll out
            </button>
          )}
          <RowAction href={logsHref(d)} icon={ScrollText} label="Logs" />
          <RowAction
            href={`/platform/integrations/i/${encodeURIComponent(d.integrationId)}`}
            icon={SlidersHorizontal}
            label="Manage"
          />
          <RowAction
            href={`/platform/i/${encodeURIComponent(d.integrationId)}`}
            icon={Pencil}
            label="Edit"
          />
        </div>
      </div>

      <DeploymentPills tag={d.tag} tracing={d.tracing} className="mt-2" />

      {d.reason && (
        <div className="mt-2 flex items-start gap-1 text-xs text-red-500">
          <AlertTriangle size={12} className="mt-0.5 shrink-0" />
          <span className="break-words">{d.reason}</span>
        </div>
      )}

      {d.externalUrl && (
        <a
          href={d.externalUrl}
          target="_blank"
          rel="noreferrer"
          className="mt-2 inline-flex items-center gap-1 truncate font-mono text-xs text-sky-600 hover:underline dark:text-sky-400"
        >
          <ExternalLink size={11} className="shrink-0" />
          {bareHost(d.externalUrl)}
        </a>
      )}

      <div className="mt-3 border-t border-black/5 pt-2 dark:border-white/10">
        <PodLines
          pods={pods}
          onOpenLogs={onOpenLogs ? (pod) => onOpenLogs(d, pod) : undefined}
        />
      </div>
    </li>
  );
}

/** A small labelled link button for a row's actions. */
function RowAction({
  href,
  icon: Icon,
  label,
}: {
  href: string;
  icon: typeof ScrollText;
  label: string;
}) {
  return (
    <Link
      href={href}
      className="inline-flex items-center gap-1.5 rounded-md border border-black/10 px-2.5 py-1 text-xs font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] hover:text-zinc-900 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06] dark:hover:text-zinc-100"
    >
      <Icon size={13} />
      {label}
    </Link>
  );
}
