"use client";

import { useEffect, useState } from "react";
import { AppPicker } from "@/app/components/AppPicker";
import { listAllDeployments } from "@/app/model/orchestrator";
import { listTraceApps } from "@/app/model/traces";
import { NO_TARGET, type WatchTarget } from "./target";

/**
 * Which app the watch is about — the first thing asked, because it is the first
 * thing anybody has decided.
 *
 * The list is what is **deployed**, not what has already reported.
 *
 * It was the other way round, on the reasoning that an app which has never
 * reported cannot be alerted on. That reasoning was wrong twice over. Traces are
 * only one of three sources — an app with tracing switched off still has logs and
 * pod stats, and neither is represented in the trace table at all. And even for
 * traces it defeats the point: the watch you most want is the one armed before
 * the first failure, which is exactly the moment there is nothing to have seen.
 * A picker drawn from telemetry offers you an app only once you no longer need
 * to be told about it.
 *
 * Telemetry is still read, but only to annotate: it orders the apps that are
 * actually live above the ones that are quiet, and marks the quiet ones so
 * nobody writes a condition against an app with nothing to preview it against.
 *
 * What is emphatically NOT used for that is the deployment's `tracing` flag,
 * which looks like the right signal and is not. Dr. Octo runs with tracing off
 * and has produced two hundred traces regardless — the flag records a per-
 * deployment switch, not whether anything has arrived — so a row marked from it
 * would tell you a trace condition could never read anything, about an app where
 * it plainly can. What has actually been recorded is the only honest answer, and
 * it is the one the metric picker one step down already gives.
 *
 * The same searchable popover every other page uses to choose an app. Versions
 * are collapsed, unlike on traces: a watch is about the app across its rollouts,
 * and a threshold that stopped applying when somebody deployed is the failure
 * this is trying to avoid.
 */

/**
 * How far back to look for signs of life.
 *
 * Explicit, and wide. Leaving both bounds off does not mean "all time" — the
 * service defaults an unbounded trace query to the last 24 hours — so the
 * previous `{ from: undefined, to: undefined }` quietly asked for one day while
 * its comment claimed a watch outlives a day.
 */
const SEEN_WITHIN_DAYS = 90;

interface AppChoice {
  key: string;
  integrationId: string;
  deploymentId: string;
  appName: string;
  /** When it last produced a trace, if it ever has. */
  lastSeenAt: string | null;
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
      const from = new Date(
        Date.now() - SEEN_WITHIN_DAYS * 24 * 60 * 60 * 1000,
      ).toISOString();
      // The registry is the list; telemetry only decorates it, so a failure to
      // read traces must not empty the picker.
      const [deployments, seen] = await Promise.all([
        listAllDeployments(),
        listTraceApps({ from, to: undefined }).then(
          (page) => page.items,
          () => [],
        ),
      ]);
      if (stopped) return;
      setApps(collapse(deployments, seen));
    };
    load()
      .catch((e) => {
        if (!stopped) setError((e as Error).message);
      })
      .finally(() => {
        if (!stopped) setLoading(false);
      });
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
            {!app.lastSeenAt && (
              <span
                title={`Nothing has been recorded for this app in the last ${SEEN_WITHIN_DAYS} days. A watch on it is still worth writing — the first failure is the one you want to hear about — but there is nothing yet to preview it against.`}
                className="shrink-0 rounded-full bg-zinc-500/15 px-2 py-0.5 text-[11px] text-zinc-600 dark:text-zinc-400"
              >
                no data yet
              </span>
            )}
          </span>
        )}
        label="Application"
        placeholder="Search apps…"
        loading={loading}
        empty={
          <p className="px-3 py-6 text-center text-xs text-zinc-500">
            Nothing is deployed yet, so there is nothing to watch.
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
 * One row per integration rather than per deployment or per version.
 *
 * Traces splits by version because a cost belongs to one or the other. A watch
 * does not: it is about the app across its rollouts, and a threshold that
 * stopped applying the moment somebody deployed is exactly the silence this
 * feature is meant to prevent. The most recent deployment wins the ids.
 *
 * Live apps sort first, most recently seen at the top, because on an
 * installation with a long history of retired integrations those are the ones
 * anybody is here to watch. The rest follow by name, which is a stable order to
 * search through rather than an interesting one.
 */
function collapse(
  deployments: Array<{
    id: string;
    integrationId: string;
    name: string;
    lastUpdated: string;
  }>,
  seen: Array<{ integrationId: string; appName: string; lastSeenAt: string }>,
): AppChoice[] {
  const lastSeen = new Map<string, string>();
  for (const item of seen) {
    for (const key of [item.integrationId, item.appName]) {
      if (!key) continue;
      const existing = lastSeen.get(key);
      if (!existing || existing < item.lastSeenAt) {
        lastSeen.set(key, item.lastSeenAt);
      }
    }
  }

  const newest = new Map<string, (typeof deployments)[number]>();
  for (const d of deployments) {
    const key = d.integrationId || d.id;
    const held = newest.get(key);
    if (!held || held.lastUpdated < d.lastUpdated) newest.set(key, d);
  }

  const byIntegration = [...newest].map(([key, d]) => ({
    key,
    integrationId: d.integrationId,
    deploymentId: d.id,
    appName: d.name,
    lastSeenAt: lastSeen.get(d.integrationId) ?? lastSeen.get(d.name) ?? null,
  }));

  return byIntegration.sort((a, b) => {
    if (a.lastSeenAt && b.lastSeenAt)
      return b.lastSeenAt.localeCompare(a.lastSeenAt);
    if (a.lastSeenAt) return -1;
    if (b.lastSeenAt) return 1;
    return a.appName.localeCompare(b.appName);
  });
}

export { NO_TARGET };
