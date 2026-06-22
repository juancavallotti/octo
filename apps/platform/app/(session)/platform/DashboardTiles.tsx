"use client";

import Link from "next/link";
import { ExternalLink, Pencil, SlidersHorizontal, type LucideIcon } from "lucide-react";
import type { Deployment, DeploymentStatus } from "@/app/model/orchestrator";

/** A deployment paired with the name of the integration it belongs to. */
export type DeployedTile = Deployment & { integrationName: string };

const STATUS_DOT: Record<DeploymentStatus, string> = {
  running: "bg-emerald-500",
  pending: "bg-amber-500",
  failed: "bg-red-500",
};

const STATUS_TEXT: Record<DeploymentStatus, string> = {
  running: "text-emerald-600 dark:text-emerald-400",
  pending: "text-amber-600 dark:text-amber-400",
  failed: "text-red-600 dark:text-red-400",
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

/** A quick-action launcher tile in the shortcuts row. */
export function ShortcutTile({
  href,
  icon: Icon,
  title,
  subtitle,
  accent,
  external,
}: {
  href: string;
  icon: LucideIcon;
  title: string;
  subtitle: string;
  accent?: boolean;
  /** Open in a new tab via a plain anchor (for off-app links like the docs). */
  external?: boolean;
}) {
  const className = `group flex items-center gap-3 rounded-xl border p-4 transition-colors ${
    accent
      ? "border-sky-500/30 bg-sky-500/[0.07] hover:bg-sky-500/[0.12]"
      : "border-black/10 bg-white/40 hover:bg-black/[0.03] dark:border-white/10 dark:bg-zinc-900/30 dark:hover:bg-white/[0.04]"
  }`;
  const inner = (
    <>
      <span
        className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ${
          accent
            ? "bg-sky-600 text-white"
            : "bg-black/[0.05] text-zinc-600 dark:bg-white/[0.08] dark:text-zinc-300"
        }`}
      >
        <Icon size={18} />
      </span>
      <span className="min-w-0">
        <span className="block text-sm font-medium">{title}</span>
        <span className="block truncate text-xs text-zinc-500 dark:text-zinc-400">
          {subtitle}
        </span>
      </span>
    </>
  );

  if (external) {
    return (
      <a href={href} target="_blank" rel="noreferrer" className={className}>
        {inner}
      </a>
    );
  }
  return (
    <Link href={href} className={className}>
      {inner}
    </Link>
  );
}

/**
 * A status tile for one deployment. The tile itself isn't a link; its two corner
 * actions are — "Manage" opens the integration in the management view (its
 * folder, deployments, secrets), "Edit" opens it in the editor.
 */
export function DeploymentTile({ d }: { d: DeployedTile }) {
  const age = relativeAge(d.createdAt);
  const desired = d.desiredReplicas || d.replicas;
  return (
    <article
      className="flex flex-col gap-3 rounded-xl border border-black/10 bg-white/40 p-4 dark:border-white/10 dark:bg-zinc-900/30"
      title={d.id}
    >
      <div className="flex items-start gap-2">
        <h3 className="min-w-0 flex-1 truncate text-sm font-semibold">
          {d.integrationName}
        </h3>
        <span
          className={`inline-flex items-center gap-1.5 text-xs font-medium capitalize ${
            STATUS_TEXT[d.status] ?? "text-zinc-500"
          }`}
        >
          <span
            className={`h-1.5 w-1.5 rounded-full ${STATUS_DOT[d.status] ?? "bg-zinc-400"}`}
          />
          {d.status}
        </span>
      </div>

      <div className="flex items-center gap-3 text-xs text-zinc-500">
        <span>
          <span className="font-medium text-zinc-700 dark:text-zinc-200">
            {d.readyReplicas}
          </span>
          /{desired} ready
        </span>
        {age && <span title="Age">· {age}</span>}
      </div>

      {d.reason && <p className="line-clamp-2 text-xs text-red-500">{d.reason}</p>}

      {d.externalUrl && (
        <a
          href={d.externalUrl}
          target="_blank"
          rel="noreferrer"
          className="inline-flex items-center gap-1 truncate font-mono text-xs text-sky-600 hover:underline dark:text-sky-400"
        >
          <ExternalLink size={11} className="shrink-0" />
          {bareHost(d.externalUrl)}
        </a>
      )}

      <div className="mt-auto flex items-center justify-end gap-2 pt-1">
        <TileAction
          href={`/platform/integrations?integration=${encodeURIComponent(d.integrationId)}`}
          icon={SlidersHorizontal}
          label="Manage"
        />
        <TileAction
          href={`/platform/i/${encodeURIComponent(d.integrationId)}`}
          icon={Pencil}
          label="Edit"
        />
      </div>
    </article>
  );
}

/** A small labelled link button used for a deployment tile's corner actions. */
function TileAction({
  href,
  icon: Icon,
  label,
}: {
  href: string;
  icon: LucideIcon;
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

/** A centered empty/placeholder block for the deployments area. */
export function EmptyState({
  icon: Icon,
  title,
  body,
}: {
  icon: LucideIcon;
  title: string;
  body: string;
}) {
  return (
    <div className="flex flex-col items-center gap-2 rounded-xl border border-dashed border-black/10 px-6 py-12 text-center dark:border-white/10">
      <Icon size={22} className="text-zinc-400" />
      <p className="text-sm font-medium">{title}</p>
      <p className="max-w-sm text-xs text-zinc-500 dark:text-zinc-400">{body}</p>
    </div>
  );
}
