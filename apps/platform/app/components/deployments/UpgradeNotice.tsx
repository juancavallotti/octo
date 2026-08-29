"use client";

import { ArrowUp } from "lucide-react";
import type { DeployedTile } from "@/app/(session)/platform/DashboardTiles";

/**
 * Which deployments are running an older octo runtime than this install deploys
 * now, said once at the top of the page.
 *
 * The pills already mark them individually, but a fleet's answer to "what is
 * stale" is not something anybody should have to assemble by scanning a grid —
 * and the deployments that need attention are usually the ones nobody has looked
 * at in months, which are the furthest down it.
 *
 * Rolling one over from here opens the same dialog its own card does, and lands
 * on the version picker, because moving a deployment to a new runtime is a
 * rollout — there is no separate upgrade operation, and inventing a button that
 * implied one would be describing something the platform does not do.
 */
export default function UpgradeNotice({
  behind,
  currentRuntime,
  onRollOut,
}: {
  behind: DeployedTile[];
  currentRuntime: string;
  onRollOut?: (d: DeployedTile) => void;
}) {
  return (
    <section className="mt-4 rounded-lg border border-amber-500/25 bg-amber-500/[0.06] px-3 py-2.5">
      <div className="flex items-center gap-2">
        <ArrowUp size={14} className="shrink-0 text-amber-600 dark:text-amber-400" />
        <h2 className="text-sm font-medium">
          {behind.length === 1
            ? "1 deployment is on an older runtime"
            : `${behind.length} deployments are on an older runtime`}
        </h2>
        <span className="text-xs text-zinc-500 dark:text-zinc-400">
          this install deploys 🐙 {currentRuntime}
        </span>
      </div>
      <ul className="mt-2 flex flex-wrap gap-1.5">
        {behind.map((d) => (
          <li key={d.id}>
            <button
              type="button"
              disabled={!onRollOut}
              onClick={() => onRollOut?.(d)}
              title={`Roll ${d.integrationName} over to ${currentRuntime}`}
              className="inline-flex items-center gap-1.5 rounded-full border border-amber-500/30 bg-white/50 px-2 py-0.5 text-xs text-zinc-700 transition-colors hover:bg-amber-500/10 disabled:cursor-default disabled:opacity-60 dark:bg-transparent dark:text-zinc-200"
            >
              <span className="max-w-[14rem] truncate">{d.integrationName}</span>
              <span className="font-mono text-[10px] text-amber-700 dark:text-amber-400">
                {d.runtimeVersion}
              </span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}
