"use client";

import type { LucideIcon } from "lucide-react";

/**
 * The headline-counter tile and its formatters, shared by every page that reports
 * numbers about a running dependency — the broker monitor at /platform/queues and
 * the storage view under /platform/objects.
 *
 * Shared rather than copied because these pages are read side by side when
 * something is wrong, and a tile that rounded bytes differently on one of them
 * would make two readings of the same install look like a discrepancy.
 */

/** Group digits for readability; counts and byte values can run large. */
export function num(n: number): string {
  return n.toLocaleString();
}

/** Humanize a byte count (1024-based) to a compact, readable string. */
export function bytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

/** Render a 0..1 ratio as a whole-number percentage. */
export function percent(ratio: number): string {
  return `${Math.round(ratio * 100)}%`;
}

/**
 * Humanize a duration in seconds, to the two units that matter at that scale — an
 * uptime of "3d 4h" answers "has it restarted recently" without making the reader
 * divide anything.
 */
export function duration(seconds: number): string {
  if (seconds < 60) return `${Math.floor(seconds)}s`;
  const m = Math.floor(seconds / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

/**
 * One headline counter tile.
 *
 * The icon carries the direction of the counter — in against out, live against
 * lifetime — which is the distinction a reader scanning eight near-identical tiles
 * has to make, and the one the labels alone make slowest.
 */
export function Stat({
  icon: Icon,
  label,
  value,
  alert,
  hint,
}: {
  icon: LucideIcon;
  label: string;
  value: string;
  /** Render the value in the alert color (e.g. nonzero slow consumers). */
  alert?: boolean;
  /** A second line under the value, for the denominator a number needs to be read. */
  hint?: string;
}) {
  return (
    <div className="rounded-xl border border-black/10 bg-white/40 p-4 dark:border-white/10 dark:bg-zinc-900/30">
      <div className="flex items-center gap-1.5 text-xs text-zinc-500 dark:text-zinc-400">
        <Icon
          size={13}
          aria-hidden
          className={`shrink-0 ${alert ? "text-red-500" : "text-zinc-400"}`}
        />
        <span className="truncate">{label}</span>
      </div>
      <div
        className={`mt-1 text-lg font-semibold tabular-nums ${
          alert ? "text-red-500" : ""
        }`}
      >
        {value}
      </div>
      {hint ? (
        <div className="mt-0.5 truncate text-xs text-zinc-400 dark:text-zinc-500">
          {hint}
        </div>
      ) : null}
    </div>
  );
}
