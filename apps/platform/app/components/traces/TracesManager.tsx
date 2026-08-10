"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Waypoints } from "lucide-react";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import type { TraceApp, TraceSummary } from "@/app/model/traces";
import TraceAppList from "./TraceAppList";
import TraceFilters from "./TraceFilters";
import TraceList from "./TraceList";
import {
  buildHref,
  parsePathname,
  readFilters,
  windowFor,
  writeFilters,
  type TraceSelection,
} from "./query";
import { useTraceApps } from "./useTraceApps";
import { useTraceList } from "./useTraceList";

/**
 * The `/platform/traces` route: the apps that published traces on the left, one
 * app's traces in the middle, and the selected trace's waterfall on the right.
 *
 * The selection lives in the URL path and the filters in its query string, and
 * the manager lives in the route's *layout*, so moving between apps and traces
 * updates the path without remounting this — the same arrangement the
 * integrations manager uses, and the reason opening a trace does not re-fetch the
 * app list behind it.
 */
export default function TracesManager({
  userMenu,
}: {
  /** Server-rendered account tile, shown in the shared header. */
  userMenu?: React.ReactNode;
} = {}) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const selection = useMemo(() => parsePathname(pathname), [pathname]);

  // Filters start from the URL; thereafter the URL follows them.
  const [filters, setFilters] = useState(() =>
    readFilters(new URLSearchParams(searchParams.toString())),
  );
  // The window is resolved to absolute bounds once per change, not per render: a
  // window that moved every render would be a new query every render.
  const [now, setNow] = useState(() => Date.now());
  const bounds = useMemo(() => windowFor(filters.window, now), [filters.window, now]);

  useEffect(() => {
    const qs = writeFilters(filters);
    router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
  }, [filters, pathname, router]);

  const apps = useTraceApps(bounds);

  // Null asks for nothing, which is what no app selected means — and is not the
  // same as an app with no traces.
  const query = useMemo(
    () =>
      selection.deploymentId
        ? {
            deploymentId: selection.deploymentId,
            ...(selection.appVersion ? { appVersion: selection.appVersion } : {}),
            ...bounds,
            statuses: filters.statuses,
            hasLlm: filters.hasLlm,
            ...(filters.minDurationMs ? { minDurationMs: filters.minDurationMs } : {}),
            ...(filters.q ? { q: filters.q } : {}),
          }
        : null,
    [selection.deploymentId, selection.appVersion, bounds, filters],
  );
  const list = useTraceList(query);

  const go = useCallback(
    (next: TraceSelection) => router.push(buildHref(next), { scroll: false }),
    [router],
  );

  const selectApp = useCallback(
    (app: TraceApp) =>
      // Selecting an app closes whatever trace was open: a trace id belongs to
      // one app's list, and carrying it across would address a trace that is not
      // in the list now on screen.
      go({
        deploymentId: app.deploymentId,
        appVersion: app.appVersion || null,
        traceId: null,
      }),
    [go],
  );

  const selectTrace = useCallback(
    (trace: TraceSummary) => go({ ...selection, traceId: trace.traceId }),
    [go, selection],
  );

  // Re-resolving the window is what a refresh means here: both panes are
  // measured over it, so moving it moves them together.
  const refresh = useCallback(() => setNow(Date.now()), []);

  const error = apps.error ?? list.error;

  return (
    <div className="flex h-full flex-col">
      <AppHeader userMenu={userMenu}>
        <ManagementNav />
      </AppHeader>

      {error && (
        <p className="border-b border-red-500/20 bg-red-500/5 px-4 py-2 text-sm text-red-500">
          {error}
        </p>
      )}

      <div className="flex min-h-0 flex-1">
        <TraceAppList
          apps={apps.apps}
          window={apps.window}
          loading={apps.loading}
          selectedId={selection.deploymentId}
          selectedVersion={selection.appVersion}
          onSelect={selectApp}
          onRefresh={refresh}
        />

        <div className="flex w-96 shrink-0 flex-col border-r border-black/10 dark:border-white/10">
          <TraceFilters
            value={filters}
            disabled={!selection.deploymentId}
            onChange={setFilters}
          />
          <div className="min-h-0 flex-1 overflow-y-auto">
            {selection.deploymentId ? (
              <TraceList
                traces={list.traces}
                selectedId={selection.traceId}
                loading={list.loading}
                loadingMore={list.loadingMore}
                hasMore={list.older !== null}
                onSelect={selectTrace}
                onLoadMore={list.loadMore}
              />
            ) : (
              <p className="px-3 py-4 text-xs text-zinc-400">
                Choose an app to see its traces.
              </p>
            )}
          </div>
        </div>

        <div className="min-w-0 flex-1 overflow-y-auto p-6">
          {selection.traceId ? (
            <p className="text-sm text-zinc-400">
              The waterfall for this trace appears here.
            </p>
          ) : (
            <EmptyState
              icon={Waypoints}
              title={error ? "Traces unavailable" : "Pick a trace"}
              body={
                error
                  ? "The platform can't reach the trace service. Set LOGS_URL to enable it."
                  : "Choose an execution to see everything that happened on it — every flow, block and model call, with what each one took."
              }
            />
          )}
        </div>
      </div>
    </div>
  );
}
