"use client";

import { useState } from "react";
import { ChevronRight, RotateCcw, ScrollText } from "lucide-react";
import type { PodStatus } from "@/app/model/orchestrator";

/**
 * A deployment's pods, collapsed by default, each with a readiness dot, its
 * restart count and a link to its logs.
 *
 * It owns its own open/closed state, which is the reason it is a component
 * rather than a fragment of the row: nothing above it needs to know whether a
 * row's pods are showing, and the row has enough to hold already.
 */
export default function PodList({
  pods,
  onOpenLogs,
}: {
  pods: PodStatus[];
  onOpenLogs: (podName: string) => void;
}) {
  const [podsOpen, setPodsOpen] = useState(false);

  return (
    <div className="mt-2 border-t border-zinc-100 pt-2 dark:border-zinc-800/70">
      <button
        type="button"
        onClick={() => setPodsOpen((o) => !o)}
        aria-expanded={podsOpen}
        className="flex w-full items-center gap-1 text-[10px] font-semibold uppercase tracking-wide text-zinc-400 transition-colors hover:text-zinc-600 dark:hover:text-zinc-300"
      >
        <ChevronRight
          size={12}
          className={`transition-transform ${podsOpen ? "rotate-90" : ""}`}
        />
        Pods
        <span className="font-sans normal-case text-zinc-400">
          ({pods.length})
        </span>
      </button>
      {podsOpen && (
        <div className="mt-1.5 space-y-1">
          {pods.map((p) => (
            <div key={p.name} className="flex items-center gap-2">
              <span
                aria-hidden
                className={`h-2 w-2 shrink-0 rounded-full ${
                  p.ready
                    ? "bg-emerald-500"
                    : p.phase === "Running"
                      ? "bg-amber-500"
                      : "bg-zinc-400"
                }`}
                title={p.ready ? "Ready" : p.phase}
              />
              <span className="min-w-0 flex-1 truncate font-mono text-xs text-zinc-600 dark:text-zinc-300">
                {p.name}
              </span>
              {p.restarts > 0 && (
                <span
                  className="inline-flex shrink-0 items-center gap-0.5 text-xs text-amber-600 dark:text-amber-400"
                  title="Container restarts"
                >
                  <RotateCcw size={11} />
                  {p.restarts}
                </span>
              )}
              <button
                type="button"
                onClick={() => onOpenLogs(p.name)}
                aria-label={`View logs for ${p.name}`}
                title="View logs"
                className="shrink-0 rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-600 dark:hover:bg-white/[0.08] dark:hover:text-zinc-300"
              >
                <ScrollText size={14} />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
