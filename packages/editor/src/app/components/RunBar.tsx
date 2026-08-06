"use client";

import { Play, Square } from "lucide-react";
import { useRun } from "../run/RunContext";
import { useEditorState } from "../state/editorState";
import { issueMessages } from "../model/validate";
import TestRunButton from "./TestRunButton";

/**
 * The RUN control in the header. It only appears when a runner binary is available (the
 * editor was launched with `task dev`).
 *
 * It is contextual: on the Testing tab it runs the TESTS, because that is the only kind of
 * run that tab is about. Everywhere else it starts and stops the integration, enabled only
 * when the document passes the validity gate; when blocked, the issues are surfaced in the
 * button's tooltip.
 *
 * On the Testing tab the Stop button goes with it, even while a runner is live. A running
 * integration is not hidden by that — the console keeps its green dot and its log stream —
 * and Stop is one click away on any other view mode.
 */
export default function RunBar() {
  const run = useRun();
  const { state } = useEditorState();

  // No RunProvider mounted, or no runner available => no RUN control. Note that a missing
  // dolphin does NOT land here: the host requires octo for a test run too, so `available`
  // gates both, and the test button explains a missing dolphin itself.
  if (!run || !run.available) return null;

  if (state.viewMode === "testing") {
    return (
      <div className="ml-auto flex items-center gap-2">
        <TestRunButton />
      </div>
    );
  }

  const { running, busy, validation, error, reloadsOnSave, start, stop } = run;

  if (running) {
    return (
      <div className="ml-auto flex items-center gap-2">
        {error && <span className="text-xs text-red-500">{error}</span>}
        {/* Said plainly while the app is running, because it is the one thing about this
            host a user cannot infer from the screen: their edits are not reaching the
            running app until they save. */}
        {reloadsOnSave && (
          <span className="text-xs text-neutral-500" title="Edits reach the running app when you save it, not as you type.">
            reloads on save
          </span>
        )}
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
    ? `Fix before running:\n• ${issueMessages(validation).join("\n• ")}`
    : reloadsOnSave
      ? "Run this integration; it reloads when you save"
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
