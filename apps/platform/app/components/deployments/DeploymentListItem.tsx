"use client";

import Link from "next/link";
import {
  AlertTriangle,
  Clock,
  ExternalLink,
  Pencil,
  RotateCcw,
  ScrollText,
  SlidersHorizontal,
  Waypoints,
} from "lucide-react";
import type {
  DeploymentStatus,
  PodStatus,
} from "@/app/model/orchestrator";
import type { DeployedTile } from "@/app/(session)/platform/DashboardTiles";
import ReplicaStepper from "@/app/components/integrations/ReplicaStepper";

/**
 * One deployment in the deployments page's list (as opposed to the dashboard's
 * tile grid). Unlike the read-only tile, this row shows the live per-pod state and
 * lets you scale the deployment and jump to its logs in context — the management
 * actions the dedicated page is for. Scaling is delegated to the parent (which
 * performs the call and refreshes), mirroring the in-context DeploymentRow.
 */

const STATUS_STYLES: Record<DeploymentStatus, string> = {
  running: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  pending: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  failed: "bg-red-500/15 text-red-600 dark:text-red-400",
};

// Pod phases come straight from Kubernetes; colour the healthy/terminal ones.
const PHASE_DOT: Record<string, string> = {
  Running: "bg-emerald-500",
  Pending: "bg-amber-500",
  Succeeded: "bg-sky-500",
  Failed: "bg-red-500",
  Unknown: "bg-zinc-400",
};

/** Compact relative age (e.g. "3m", "2h", "5d") from an RFC3339 timestamp. */
function relativeAge(iso?: string): string | null {
  if (!iso) return null;
  const secs = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (!Number.isFinite(secs) || secs < 0) return null;
  if (secs < 60) return `${secs}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m`;
  if (secs < 86400) return `${Math.floor(secs / 3600)}h`;
  return `${Math.floor(secs / 86400)}d`;
}

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

function PodLine({ pod }: { pod: PodStatus }) {
  return (
    <li className="flex items-center gap-2 text-xs">
      <span
        className={`h-1.5 w-1.5 shrink-0 rounded-full ${PHASE_DOT[pod.phase] ?? "bg-zinc-400"}`}
        title={pod.phase}
      />
      <span className="min-w-0 flex-1 truncate font-mono text-zinc-600 dark:text-zinc-300">
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
    </li>
  );
}

export default function DeploymentListItem({
  deployment: d,
  busy,
  onScale,
}: {
  deployment: DeployedTile;
  busy: boolean;
  onScale: (d: DeployedTile, replicas: number) => void;
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
        {pods.length === 0 ? (
          <p className="text-xs text-zinc-400">No pods reported.</p>
        ) : (
          <ul className="space-y-1.5">
            {pods.map((p) => (
              <PodLine key={p.name} pod={p} />
            ))}
          </ul>
        )}
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
