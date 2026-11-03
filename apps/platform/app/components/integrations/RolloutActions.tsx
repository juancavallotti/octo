"use client";

import { FlaskConical, GitCompareArrows } from "lucide-react";

/**
 * The rollout dialog's footer: cancel, roll out, and — when the host offers it —
 * test & roll out.
 */
export default function RolloutActions({
  actionLabel,
  busy,
  canRollout,
  canTest,
  testBlocked,
  onClose,
  onSubmit,
  onTestSubmit,
}: {
  actionLabel: string;
  busy: boolean;
  canRollout: boolean;
  /** Whether testing first is possible; undefined hides the button. */
  canTest?: boolean;
  /** Why testing first is unavailable, shown on the button; undefined when it is. */
  testBlocked?: string;
  onClose: () => void;
  onSubmit: () => void;
  onTestSubmit: () => void;
}) {
  return (
    <footer className="flex justify-end gap-2 border-t border-black/10 px-4 py-3 dark:border-white/10">
      <button
        type="button"
        onClick={onClose}
        disabled={busy}
        className="rounded-md px-3 py-1 text-sm text-zinc-600 transition-colors hover:bg-black/[0.06] disabled:opacity-50 dark:text-zinc-300 dark:hover:bg-white/[0.08]"
      >
        Cancel
      </button>
      {canTest !== undefined && (
        <button
          type="button"
          onClick={onTestSubmit}
          disabled={!canTest}
          title={
            testBlocked ??
            "Run this version's test suites on this install's runtime, and roll out only if they pass."
          }
          className="inline-flex items-center gap-1.5 rounded-md border border-sky-600/40 px-3 py-1 text-sm font-medium text-sky-700 transition-colors hover:bg-sky-500/10 disabled:opacity-50 dark:text-sky-300"
        >
          <FlaskConical size={14} />
          Test &amp; {actionLabel.toLowerCase()}
        </button>
      )}
      <button
        type="button"
        onClick={onSubmit}
        disabled={!canRollout}
        className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white transition-colors hover:bg-sky-500 disabled:opacity-50"
      >
        <GitCompareArrows size={14} />
        {actionLabel}
      </button>
    </footer>
  );
}
