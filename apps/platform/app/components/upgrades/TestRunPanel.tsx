"use client";

import type { TestRun } from "@/app/model/upgrades";
import TestRunReport from "./TestRunReport";
import { useTestRun } from "./useTestRun";

/** A dialog body following a test run to its end. */
export default function TestRunPanel({
  initial,
  editorHref,
  onFinished,
  onClose,
}: {
  initial: TestRun;
  /** Where the integration opens in the editor, offered when the run does not pass. */
  editorHref?: string;
  onFinished?: (run: TestRun) => void;
  onClose: () => void;
}) {
  const { run, error } = useTestRun(initial, onFinished);
  return (
    <>
      <div className="flex max-h-[70vh] flex-col gap-3 overflow-y-auto px-4 py-4">
        <TestRunReport run={run} editorHref={editorHref} />
        {run.status === "running" && (
          <p className="text-xs text-zinc-500 dark:text-zinc-400">
            {run.rolloutOnPass
              ? "The deployment is rolled out when every case passes. You can close this; the run carries on."
              : "You can close this; the run carries on."}
          </p>
        )}
        {error && (
          <p className="text-xs text-amber-600 dark:text-amber-400">{error}</p>
        )}
      </div>
      <footer className="flex justify-end gap-2 border-t border-black/10 px-4 py-3 dark:border-white/10">
        <button
          type="button"
          onClick={onClose}
          className="rounded-md px-3 py-1 text-sm text-zinc-600 transition-colors hover:bg-black/[0.06] dark:text-zinc-300 dark:hover:bg-white/[0.08]"
        >
          Close
        </button>
      </footer>
    </>
  );
}
