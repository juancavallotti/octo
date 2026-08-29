"use client";

import { useState } from "react";
import { ArrowUp, Check, Copy, ExternalLink, Tag, Waypoints } from "lucide-react";
import type { DeploymentStatus } from "@/app/model/orchestrator";
import { bareHost } from "./podStats";
import { needsUpgrade } from "@/app/lib/runtimeRelease";

/**
 * The presentational pieces a deployment row is assembled from: its status badge,
 * the version, runtime and tracing pills, a copy-to-clipboard button, and a
 * labelled address line.
 *
 * None of them knows what a deployment is — they take a status, a string, a
 * label — which is why they sit apart from the row that arranges them. The two
 * cards that show a deployment (in the integration, and on the deployments page)
 * both wear the pills, and a pill that read differently in the two places would
 * be two facts about one deployment.
 */

const STATUS_STYLES: Record<DeploymentStatus, string> = {
  running: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  pending: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  failed: "bg-red-500/15 text-red-600 dark:text-red-400",
};

export function StatusBadge({ status }: { status: DeploymentStatus }) {
  const cls = STATUS_STYLES[status] ?? "bg-zinc-500/15 text-zinc-500";
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium capitalize ${cls}`}
    >
      {status}
    </span>
  );
}

/** The version tag a deployment was cut from. */
export function VersionPill({ tag }: { tag: string }) {
  return (
    <span
      className="inline-flex items-center gap-1 rounded-full bg-sky-500/15 px-2 py-0.5 text-xs font-medium text-sky-600 dark:text-sky-400"
      title={`Version ${tag}`}
    >
      <Tag size={10} />
      {tag}
    </span>
  );
}

/**
 * The octo runtime the deployment's pods are running, under the octopus — the same
 * mark the runtime greets you with when it starts. Distinct from VersionPill,
 * which is the integration's own version: a deployment keeps the runtime image it
 * was created with until it is rolled over, so a cluster commonly runs several at
 * once — and which one a misbehaving deployment is on is the first thing asked
 * when troubleshooting it.
 */
export function RuntimePill({
  version,
  image,
  behind,
}: {
  version: string;
  /** Full image reference, shown on hover; the pill itself wears the tag. */
  image?: string;
  /**
   * The runtime this install deploys now, when this deployment is not on it. Its
   * presence is what turns the pill into a prompt: a deployment keeps the runtime
   * it was created with until somebody rolls it over, and nothing else on the page
   * says that has fallen behind.
   */
  behind?: string;
}) {
  const tone = behind
    ? "bg-amber-500/15 text-amber-600 dark:text-amber-400"
    : "bg-zinc-500/15 text-zinc-600 dark:text-zinc-300";
  return (
    <span
      className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium ${tone}`}
      title={
        behind
          ? `Running octo ${version}; this install deploys ${behind}. Roll it out to move it.`
          : image
            ? `Runtime image ${image}`
            : `Runtime ${version}`
      }
    >
      <span aria-hidden>🐙</span>
      {version}
      {behind && <ArrowUp size={11} aria-label="needs upgrade" />}
    </span>
  );
}

/** Shown only when tracing is on — its absence is what "off" looks like. */
export function TracedPill() {
  return (
    <span
      className="inline-flex items-center gap-1 rounded-full bg-violet-500/15 px-2 py-0.5 text-xs font-medium text-violet-600 dark:text-violet-400"
      title="Tracing is on for this deployment — every flow, block and model call is recorded"
    >
      <Waypoints size={10} />
      Traced
    </span>
  );
}

/**
 * The row of pills describing what a deployment is running: its version, the
 * runtime carrying it, and whether it is traced. Renders nothing when it has none
 * of them, so a card with nothing to say does not grow an empty line.
 */
export function DeploymentPills({
  tag,
  tracing,
  runtimeVersion,
  runtimeImage,
  currentRuntime,
  className = "",
}: {
  tag?: string;
  tracing?: boolean;
  /** The runtime tag the pods report; absent on a workload the cluster can't see. */
  runtimeVersion?: string;
  runtimeImage?: string;
  /** The runtime this install deploys now; "" when it has no way of knowing. */
  currentRuntime?: string;
  className?: string;
}) {
  if (!tag && !tracing && !runtimeVersion) return null;
  return (
    <div className={`flex flex-wrap items-center gap-1.5 ${className}`}>
      {tag && <VersionPill tag={tag} />}
      {runtimeVersion && (
        <RuntimePill
          version={runtimeVersion}
          image={runtimeImage}
          behind={
            needsUpgrade(runtimeVersion, currentRuntime ?? "") ? currentRuntime : undefined
          }
        />
      )}
      {tracing && <TracedPill />}
    </div>
  );
}

/** A small copy-to-clipboard button with brief "copied" feedback. */
export function CopyButton({ value }: { value: string }) {
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
      {copied ? (
        <Check size={12} className="text-emerald-500" />
      ) : (
        <Copy size={12} />
      )}
    </button>
  );
}

/** A labelled address line: a tag, then the address (a link when external). */
export function AddressLine({
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
