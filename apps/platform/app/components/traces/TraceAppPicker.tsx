"use client";

import { AlertTriangle } from "lucide-react";
import type { TraceApp } from "@/app/model/traces";
import { AppPicker } from "@/app/components/AppPicker";
import { WINDOW_PRESETS, type WindowPreset } from "./query";
import { describeCost, formatAge } from "./format";

/**
 * The apps that produced traces, and what they cost — the choice you make before
 * looking at any individual trace.
 *
 * A row is a (deployment, version) pair rather than a deployment, because that is
 * how the store groups them: a rollout keeps the deployment id and changes the
 * version, and both the failures and the cost belong to one version or the other.
 * Two rows for one deployment is the honest picture of a rollout — which is also
 * why there is no second control for the version. Picking the version *is*
 * picking the app here, and splitting them would let someone address a pair the
 * store never reported.
 *
 * Dropped records are reported here and nowhere else. The marker the runtime
 * publishes when it cannot keep up carries no trace id, so nothing can say *which*
 * traces are incomplete — only that some of this app's are. Attaching that to an
 * individual trace would be an invention; leaving it out entirely would let a
 * reader draw conclusions from a set they did not know had holes in it.
 *
 * The window comes *before* the app, because that is the order the two are
 * actually decided in: the app list is counted over the window, so an app is
 * only in it — and only says "12 traces" — because of a window that was already
 * chosen. Offered afterwards it asked someone to pick from a list narrowed by
 * something they had not been shown yet, and it was shown twice for the trouble:
 * once as a label here and once as the filter bar's own control.
 */
export default function TraceAppPicker({
  apps,
  window,
  onWindowChange,
  loading,
  selectedId,
  selectedVersion,
  onSelect,
  onRefresh,
}: {
  apps: TraceApp[];
  /** How far back both the app list and the traces under it are measured. */
  window: WindowPreset;
  onWindowChange: (window: WindowPreset) => void;
  loading: boolean;
  selectedId: string | null;
  selectedVersion: string | null;
  onSelect: (app: TraceApp) => void;
  onRefresh: () => void;
}) {
  // Normalized both ways: a deployment from before version tags reports "" and
  // the path omits the segment entirely, so the two spellings of "no version"
  // have to compare equal or that app could never show as the selected one.
  const selected =
    apps.find(
      (app) =>
        app.deploymentId === selectedId &&
        (app.appVersion || null) === (selectedVersion || null),
    ) ?? null;

  return (
    <AppPicker<TraceApp>
      items={apps}
      selected={selected}
      onSelect={onSelect}
      toKey={(app) => `${app.deploymentId} ${app.appVersion}`}
      toText={(app) =>
        `${app.appName || app.deploymentId} ${app.appVersion} ${app.deploymentId}`
      }
      renderValue={(app) => <AppFace app={app} />}
      renderRow={(app) => <AppRow app={app} />}
      leading={
        <select
          aria-label="Window"
          value={window}
          onChange={(e) => onWindowChange(e.target.value as WindowPreset)}
          className="shrink-0 rounded-md border border-black/10 bg-transparent px-1.5 py-1 text-xs outline-none focus:border-black/30 dark:border-white/15 dark:focus:border-white/30"
        >
          {WINDOW_PRESETS.map((preset) => (
            <option key={preset.key} value={preset.key}>
              {preset.label}
            </option>
          ))}
        </select>
      }
      label="App"
      placeholder="Choose an app…"
      empty="No app has published a trace in this window. Tracing is off by default — turn it on for a deployment you are investigating."
      loading={loading}
      onRefresh={onRefresh}
    />
  );
}

/**
 * Name and version, which together are what was picked.
 *
 * The name is what someone recognises the app by, so it gets the room: the
 * version is a build hash with a tag on the front and cuts down to something
 * still recognisable, while a truncated name is just a letter.
 */
function AppFace({ app }: { app: TraceApp }) {
  return (
    <span className="flex items-baseline gap-2">
      <span className="min-w-0 truncate">
        {app.appName || app.deploymentId.slice(0, 8)}
      </span>
      {app.appVersion && <VersionBadge version={app.appVersion} shrink />}
    </span>
  );
}

function VersionBadge({ version, shrink }: { version: string; shrink?: boolean }) {
  return (
    <span
      title={shrink ? version : undefined}
      className={`rounded bg-black/[0.06] px-1.5 py-0.5 font-mono text-[10px] text-zinc-500 dark:bg-white/[0.08] dark:text-zinc-400 ${
        // Capped rather than merely shrunk. Weighting the shrink still let a
        // long version take enough room to clip the name by a few pixels, which
        // is all "Dr. Octo" needs to become "Dr. Oc…". Half the trigger is the
        // most a build hash may claim; the floor keeps it from vanishing under a
        // pathologically long name, so both stay readable at every width.
        shrink ? "min-w-12 max-w-[50%] truncate" : "shrink-0"
      }`}
    >
      {version}
    </span>
  );
}

function AppRow({ app }: { app: TraceApp }) {
  // The app rollup carries no call count, so "did this app call a model at all"
  // is read off the two numbers that only exist because it did.
  const cost = describeCost(
    app.costUsd,
    app.unpricedCalls,
    app.costUsd > 0 || app.unpricedCalls > 0,
  );

  return (
    <span className="flex flex-col gap-1">
      <span className="flex items-baseline gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium">
          {app.appName || app.deploymentId.slice(0, 8)}
        </span>
        {app.appVersion && <VersionBadge version={app.appVersion} />}
      </span>

      <span className="flex items-center gap-2 text-xs text-zinc-500 dark:text-zinc-400">
        <span>
          {app.traces} trace{app.traces === 1 ? "" : "s"}
        </span>
        {app.failed > 0 && <span className="text-red-500">{app.failed} failed</span>}
        <span
          title={cost.title}
          className={cost.partial ? "text-amber-600 dark:text-amber-400" : undefined}
        >
          {cost.text}
        </span>
        <span className="ml-auto shrink-0">{formatAge(app.lastSeenAt)}</span>
      </span>

      {app.droppedRecords > 0 && (
        <span
          title="The runtime could not keep up and dropped these records. The marker carries no trace id, so which traces are incomplete cannot be known — only that some are."
          className="flex items-center gap-1 text-xs text-amber-600 dark:text-amber-400"
        >
          <AlertTriangle size={11} />
          {app.droppedRecords} records dropped
        </span>
      )}
    </span>
  );
}
