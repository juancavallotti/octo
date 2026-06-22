"use client";

import { Play, Square } from "lucide-react";
import { useRun } from "@/app/run/RunContext";

/**
 * The RUN / STOP control in the header. It only appears when a runner binary is
 * available (the editor was launched with `task dev`). RUN is enabled only when
 * the document passes the validity gate; when blocked, the issues are surfaced in
 * the button's tooltip.
 */
export default function RunBar() {
  const run = useRun();

  // No RunProvider mounted, or no runner available => no RUN control.
  if (!run || !run.available) return null;

  const { running, busy, validation, error, start, stop } = run;

  if (running) {
    return (
      <div className="ml-auto flex items-center gap-2">
        {error && <span className="text-xs text-red-500">{error}</span>}
        <button
          type="button"
          onClick={stop}
          disabled={busy}
          className="inline-flex items-center gap-1.5 rounded-md bg-red-600 px-3 py-1 text-sm font-medium text-white hover:bg-red-500 disabled:opacity-50"
        >
          <Square className="h-3.5 w-3.5 fill-current" />
          Stop
        </button>
      </div>
    );
  }

  const blocked = !validation.ok;
  const title = blocked
    ? `Fix before running:\n• ${validation.issues.join("\n• ")}`
    : "Run this integration with hot reload";

  return (
    <div className="ml-auto flex items-center gap-2">
      {error && <span className="text-xs text-red-500">{error}</span>}
      <button
        type="button"
        onClick={start}
        disabled={busy || blocked}
        title={title}
        className="inline-flex items-center gap-1.5 rounded-md bg-emerald-600 px-3 py-1 text-sm font-medium text-white hover:bg-emerald-500 disabled:cursor-not-allowed disabled:opacity-40"
      >
        <Play className="h-3.5 w-3.5 fill-current" />
        Run
      </button>
    </div>
  );
}
