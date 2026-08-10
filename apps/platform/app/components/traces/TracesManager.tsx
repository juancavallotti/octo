"use client";

import { useCallback, useMemo } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Waypoints } from "lucide-react";
import AppHeader from "@/app/components/AppHeader";
import ManagementNav from "@/app/components/ManagementNav";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import type { TraceApp } from "@/app/model/traces";
import TraceAppList from "./TraceAppList";
import { buildHref, parsePathname, type TraceSelection } from "./query";
import { useTraceApps } from "./useTraceApps";

/**
 * The `/platform/traces` route: the apps that published traces on the left, one
 * app's traces in the middle, and the selected trace's waterfall on the right.
 *
 * The selection lives in the URL and the manager lives in the route's *layout*,
 * so moving between apps and traces updates the path without remounting this —
 * the same arrangement the integrations manager uses, and the reason opening a
 * trace does not re-fetch the app list behind it.
 */
export default function TracesManager({
  userMenu,
}: {
  /** Server-rendered account tile, shown in the shared header. */
  userMenu?: React.ReactNode;
} = {}) {
  const router = useRouter();
  const pathname = usePathname();
  const selection = useMemo(() => parsePathname(pathname), [pathname]);

  const { apps, window, loading, error, refresh } = useTraceApps();

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
          apps={apps}
          window={window}
          loading={loading}
          selectedId={selection.deploymentId}
          selectedVersion={selection.appVersion}
          onSelect={selectApp}
          onRefresh={refresh}
        />

        <div className="min-w-0 flex-1 overflow-y-auto p-6">
          {selection.deploymentId ? (
            <p className="text-sm text-zinc-400">
              Traces for this app appear here.
            </p>
          ) : (
            <EmptyState
              icon={Waypoints}
              title={error ? "Traces unavailable" : "Pick an app"}
              body={
                error
                  ? "The platform can't reach the trace service. Set LOGS_URL to enable it."
                  : "Choose an app on the left to see the executions it recorded. Tracing is off by default and costs throughput — turn it on for the deployment you are investigating."
              }
            />
          )}
        </div>
      </div>
    </div>
  );
}
