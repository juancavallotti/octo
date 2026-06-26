"use client";

import { useState } from "react";
import {
  AlertTriangle,
  Check,
  Clock,
  Copy,
  ExternalLink,
  RotateCcw,
  Tag,
  Trash2,
} from "lucide-react";
import type { Deployment, DeploymentStatus } from "@/app/model/orchestrator";
import ReplicaStepper from "./ReplicaStepper";

/**
 * One row in the deployments list, laid out as a small card: a header line with
 * the status badge, short id, ready/desired replicas, restarts and age plus the
 * Undeploy action, then clearly-labelled External/Internal address lines and the
 * failure reason when failed. Split out of DeploymentsSection to keep that
 * component focused on data/actions.
 */

const STATUS_STYLES: Record<DeploymentStatus, string> = {
  running: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  pending: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  failed: "bg-red-500/15 text-red-600 dark:text-red-400",
};

function StatusBadge({ status }: { status: DeploymentStatus }) {
  const cls = STATUS_STYLES[status] ?? "bg-zinc-500/15 text-zinc-500";
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium capitalize ${cls}`}
    >
      {status}
    </span>
  );
}

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

/** Total container restarts across a deployment's pods. */
function totalRestarts(d: Deployment): number {
  return (d.pods ?? []).reduce((sum, p) => sum + p.restarts, 0);
}

/** Strip the scheme so an address reads as a bare host[:port]/path. */
function bareHost(url: string): string {
  return url.replace(/^https?:\/\//, "");
}

/** A small copy-to-clipboard button with brief "copied" feedback. */
function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <button
      type="button"
      aria-label="Copy address"
      onClick={() => {
        navigator.clipboard?.writeText(value).then(
          () => {
            setCopied(true);
            setTimeout(() => setCopied(false), 1200);
          },
          () => {},
        );
      }}
      className="rounded p-0.5 text-zinc-400 transition-colors hover:bg-zinc-500/10 hover:text-zinc-600 dark:hover:text-zinc-300"
    >
      {copied ? <Check size={12} className="text-emerald-500" /> : <Copy size={12} />}
    </button>
  );
}

/** A labelled address line: a tag, then the address (a link when external). */
function AddressLine({
  label,
  value,
  href,
}: {
  label: string;
  value: string;
  href?: string;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="w-14 shrink-0 text-[10px] font-semibold uppercase tracking-wide text-zinc-400">
        {label}
      </span>
      {href ? (
        <a
          href={href}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 truncate font-mono text-xs text-sky-600 hover:underline dark:text-sky-400"
        >
          {bareHost(value)}
          <ExternalLink size={11} className="shrink-0" />
        </a>
      ) : (
        <span className="truncate font-mono text-xs text-zinc-600 dark:text-zinc-300">
          {bareHost(value)}
        </span>
      )}
      <CopyButton value={bareHost(value)} />
    </div>
  );
}

export default function DeploymentRow({
  deployment: d,
  busy,
  onScale,
  onUndeploy,
}: {
  deployment: Deployment;
  busy: boolean;
  onScale: (d: Deployment, replicas: number) => void;
  onUndeploy: (d: Deployment) => void;
}) {
  const age = relativeAge(d.createdAt);
  const restarts = totalRestarts(d);
  const desired = d.desiredReplicas || d.replicas;

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
        <button
          type="button"
          onClick={() => onUndeploy(d)}
          disabled={busy}
          aria-label="Undeploy"
          className="ml-auto rounded-md p-1 text-zinc-400 transition-colors hover:bg-red-500/10 hover:text-red-500 disabled:opacity-50"
        >
          <Trash2 size={14} />
        </button>
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
    </li>
  );
}
