"use client";

import { Minus, Plus } from "lucide-react";

/**
 * Compact desired-replica stepper for a deployment row. The − / + buttons request
 * a scale to one fewer/more replica; the parent performs the call and refreshes,
 * so the displayed count reflects the live desired count, not local state. Scaling
 * below one is disabled (undeploy removes a deployment entirely).
 */
export default function ReplicaStepper({
  desired,
  busy,
  onScale,
}: {
  desired: number;
  busy: boolean;
  onScale: (replicas: number) => void;
}) {
  const btn =
    "flex h-5 w-5 items-center justify-center text-zinc-500 transition-colors hover:text-zinc-800 disabled:opacity-40 dark:hover:text-zinc-200";
  return (
    <span
      className="inline-flex items-center rounded-md border border-black/10 dark:border-white/15"
      title="Desired replicas"
    >
      <button
        type="button"
        aria-label="Scale down"
        disabled={busy || desired <= 1}
        onClick={() => onScale(desired - 1)}
        className={btn}
      >
        <Minus size={11} />
      </button>
      <span className="min-w-[1.25rem] text-center text-xs tabular-nums text-zinc-700 dark:text-zinc-200">
        {desired}
      </span>
      <button
        type="button"
        aria-label="Scale up"
        disabled={busy}
        onClick={() => onScale(desired + 1)}
        className={btn}
      >
        <Plus size={11} />
      </button>
    </span>
  );
}
