"use client";

import { RunBar } from "@octo/editor";

/**
 * The standalone editor's top bar. Unlike the platform header there is no
 * orchestrator, auth, or folder organization — just the product mark and the RUN
 * control. The Save/Open controls (local-disk filesystem) are added alongside
 * RunBar once the filesystem capability is wired in.
 */
export default function StandaloneHeader() {
  return (
    <header className="flex items-center gap-2 border-b border-black/10 dark:border-white/10 px-4 h-12 shrink-0">
      <span className="font-semibold tracking-tight">Octo</span>
      <span className="rounded bg-black/[0.06] px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide text-zinc-500 dark:bg-white/10">
        standalone
      </span>
      <div className="ml-auto flex items-center gap-2">
        <RunBar />
      </div>
    </header>
  );
}
