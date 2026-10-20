"use client";

import { Upload } from "lucide-react";
import { HEADER_LABEL } from "@octo/editor";
import { paasBridge } from "../desktop";
import { usePlatformState } from "../platform/usePlatformState";

/**
 * Push, beside Save, when the open folder is an integration on the platform: save,
 * then replace its draft there with the folder. Like Save it reports no success of
 * its own; the platform chip says how the last push went.
 */
export default function PushButton() {
  const state = usePlatformState();
  const paas = paasBridge();
  if (!paas || !state?.link) return null;

  const running = state.activity?.state === "running";
  const label = "Push";
  return (
    <button
      type="button"
      disabled={running}
      onClick={() => void paas.push().catch(() => {})}
      aria-label={label}
      title={`Save, then push this folder to ${state.link.integrationName} on ${state.link.profile}`}
      className="inline-flex shrink-0 items-center gap-1.5 rounded-md border border-sky-600/40 px-3 py-1 text-sm font-medium text-sky-700 hover:bg-sky-500/10 disabled:cursor-not-allowed disabled:opacity-40 dark:text-sky-300"
    >
      <Upload className="h-3.5 w-3.5 shrink-0" />
      <span className={HEADER_LABEL}>{label}</span>
    </button>
  );
}
