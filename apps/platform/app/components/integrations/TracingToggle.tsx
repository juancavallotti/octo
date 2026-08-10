"use client";

import { Waypoints } from "lucide-react";

/**
 * The per-deployment tracing switch, shared by the deploy and rollout dialogs so
 * the two cannot describe the same setting differently.
 *
 * It is deliberately a plain checkbox rather than a prominent control: tracing is
 * a diagnostic you turn on for one deployment while you are looking at it, not a
 * feature to opt into by default. The warning is shown only when it is on, where
 * it is a consequence rather than a discouragement.
 */
export default function TracingToggle({
  checked,
  busy,
  onChange,
}: {
  checked: boolean;
  busy: boolean;
  onChange: (next: boolean) => void;
}) {
  return (
    <>
      <label className="flex items-center gap-2 text-sm text-zinc-600 dark:text-zinc-300">
        <input
          type="checkbox"
          checked={checked}
          disabled={busy}
          onChange={(e) => onChange(e.target.checked)}
          className="accent-sky-500"
        />
        <Waypoints size={14} />
        Trace this deployment
      </label>
      {checked && (
        <p className="mt-2 text-xs text-amber-600 dark:text-amber-400">
          Captured request bodies and variables are stored as they arrive and are
          readable by anyone signed in to the platform. Avoid tracing a deployment
          whose messages carry credentials.
        </p>
      )}
    </>
  );
}
