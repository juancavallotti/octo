"use client";

import { useState } from "react";
import { Check, Copy, ExternalLink } from "lucide-react";
import type { DeploymentStatus } from "@/app/model/orchestrator";
import { bareHost } from "./podStats";

/**
 * The presentational pieces a deployment row is assembled from: its status badge,
 * a copy-to-clipboard button, and a labelled address line.
 *
 * None of them knows what a deployment is — they take a status, a string, a
 * label — which is why they sit apart from the row that arranges them.
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
      {copied ? <Check size={12} className="text-emerald-500" /> : <Copy size={12} />}
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
