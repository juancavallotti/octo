"use client";

import { useEffect, useState } from "react";
import { AlertTriangle } from "lucide-react";
import { AppPicker } from "@/app/components/AppPicker";
import { listTraceApps, type TraceApp } from "@/app/model/traces";
import { NO_TARGET, type WatchTarget } from "./target";

/**
 * Which app the watch is about — the first thing asked, because it is the first
 * thing anybody has decided.
 *
 * The list comes from what has actually produced telemetry rather than from the
 * deployment registry, and that is the useful difference: these are the apps
 * there is something to watch, each carrying the three ids the three sources
 * need. An app that has never reported cannot be alerted on, and offering it
 * would be offering a watch that can only ever say "no data".
 *
 * The same searchable popover every other page uses to choose an app. Versions
 * are collapsed here, unlike on traces: a watch is about the app across its
 * rollouts, and a threshold that stopped applying when somebody deployed is the
 * failure this is trying to avoid.
 */

/** How far back the app list is drawn from. Wide, because a watch outlives a day. */
const WINDOW = { from: undefined, to: undefined };

interface AppChoice {
  key: string;
  integrationId: string;
  deploymentId: string;
  appName: string;
  lastSeenAt: string;
}

export function WatchTargetPicker({
  target,
  onChange,
}: {
  target: WatchTarget;
  onChange: (target: WatchTarget) => void;
}) {
  const [apps, setApps] = useState<AppChoice[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let stopped = false;
    const load = async () => {
      try {
        const page = await listTraceApps(WINDOW);
        if (!stopped) setApps(collapse(page.items));
      } catch (e) {
        if (!stopped) setError((e as Error).message);
      } finally {
        if (!stopped) setLoading(false);
      }
    };
    void load();
    return () => {
      stopped = true;
    };
  }, []);

  const selected =
    apps.find(
      (a) =>
        (target.integrationId && a.integrationId === target.integrationId) ||
        (target.deploymentId && a.deploymentId === target.deploymentId) ||
        (target.appName && a.appName === target.appName),
    ) ?? null;

  return (
    <div className="flex flex-col gap-1">
      <AppPicker<AppChoice>
        items={apps}
        selected={selected}
        onSelect={(app) =>
          onChange({
            integrationId: app.integrationId,
            deploymentId: app.deploymentId,
            appName: app.appName,
          })
        }
        toKey={(app) => app.key}
        toText={(app) => app.appName}
        renderRow={(app) => (
          <span className="flex w-full items-center justify-between gap-3">
            <span className="truncate">{app.appName}</span>
            {!app.integrationId && (
              <span
                title="This deployment could not be resolved to an integration, so a watch on it will not survive a rollout."
                className="flex shrink-0 items-center gap-1 text-amber-600 dark:text-amber-400"
              >
                <AlertTriangle size={11} />
                no integration
              </span>
            )}
          </span>
        )}
        label="Application"
        placeholder="Search apps…"
        loading={loading}
        empty={
          <p className="px-3 py-6 text-center text-xs text-zinc-500">
            Nothing has reported telemetry yet, so there is nothing to watch.
            Deploy an integration and let it run once.
          </p>
        }
      />
      {error && (
        <p className="text-xs text-red-600 dark:text-red-400">{error}</p>
      )}
      {selected === null && !loading && (
        <p className="text-xs text-zinc-500 dark:text-zinc-400">
          Everything below is measured over this app.
        </p>
      )}
    </div>
  );
}

/**
 * One row per app rather than per (app, version).
 *
 * Traces splits them because a cost belongs to one version or the other. A watch
 * does not: it is about the app across its rollouts, and a threshold that stopped
 * applying the moment somebody deployed is exactly the silence this feature is
 * meant to prevent. The most recently seen version wins the ids.
 */
function collapse(items: TraceApp[]): AppChoice[] {
  const byApp = new Map<string, AppChoice>();
  for (const item of items) {
    const key = item.integrationId || item.deploymentId;
    const existing = byApp.get(key);
    if (existing && existing.lastSeenAt >= item.lastSeenAt) continue;
    byApp.set(key, {
      key,
      integrationId: item.integrationId,
      deploymentId: item.deploymentId,
      appName: item.appName,
      lastSeenAt: item.lastSeenAt,
    });
  }
  return [...byApp.values()].sort((a, b) =>
    b.lastSeenAt.localeCompare(a.lastSeenAt),
  );
}

export { NO_TARGET };
