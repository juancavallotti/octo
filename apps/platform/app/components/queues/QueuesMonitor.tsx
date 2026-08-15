"use client";

import { useCallback, useEffect, useState } from "react";
import {
  ArrowDownToLine,
  ArrowUpFromLine,
  Download,
  History,
  Layers,
  Network,
  Plug,
  RefreshCw,
  TriangleAlert,
  Upload,
} from "lucide-react";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import { listQueueStats, type QueueStats } from "@/app/model/queues";
import { ConnectionsTable, Stat, bytes, num } from "./QueueViews";
import QueueDestinations from "./QueueDestinations";

/** How often to re-poll the broker snapshot. SSE isn't warranted for a counter. */
const POLL_MS = 5000;

/**
 * Live view of the platform's NATS broker: headline counters plus the open
 * client connections, re-polled every few seconds while the page is open. The
 * data is read-only (the monitoring HTTP service), so this only ever reads; a
 * failed/unconfigured poll surfaces the unavailable state rather than throwing.
 */
export default function QueuesMonitor() {
  const [stats, setStats] = useState<QueueStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  // Fetch without touching the spinner flag, so it's safe to call from the
  // effect (setState runs inside the promise callbacks, not synchronously).
  const load = useCallback(
    () =>
      listQueueStats().then(
        (s) => {
          setStats(s);
          setError(null);
        },
        // Keep the last good snapshot on a transient failure; just flag the error.
        (e) => setError((e as Error).message),
      ),
    [],
  );

  const refresh = useCallback(() => {
    setRefreshing(true);
    load().finally(() => setRefreshing(false));
  }, [load]);

  useEffect(() => {
    load();
    const id = setInterval(load, POLL_MS);
    return () => clearInterval(id);
  }, [load]);

  const server = stats?.server ?? null;

  return (
    <div className="min-h-0 flex-1 overflow-y-auto">
      <div className="mx-auto w-full max-w-6xl px-6 py-8">
        <div className="flex items-center gap-2">
          <h1 className="text-xl font-semibold tracking-tight">Queues</h1>
          <button
            type="button"
            onClick={refresh}
            disabled={refreshing}
            aria-label="Refresh queue stats"
            className="rounded-md p-1 text-zinc-400 transition-colors hover:bg-black/[0.05] hover:text-zinc-700 disabled:opacity-50 dark:hover:bg-white/[0.06] dark:hover:text-zinc-200"
          >
            <RefreshCw
              size={14}
              className={refreshing ? "animate-spin" : undefined}
            />
          </button>
        </div>
        <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
          {server
            ? `${server.serverName} · NATS ${server.version} · up ${server.uptime}`
            : "Live status of the platform message broker."}
        </p>

        {error && (
          <p className="mt-4 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
            {error}
          </p>
        )}

        {stats === null ? (
          error ? (
            <div className="mt-6">
              <EmptyState
                icon={Network}
                title="Queue monitoring unavailable"
                body="The platform can't reach the NATS monitoring service. Set NATS_MONITOR_URL to enable it."
              />
            </div>
          ) : (
            <p className="mt-6 text-sm text-zinc-400">Loading queue stats…</p>
          )
        ) : (
          <>
            <div className="mt-6 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4">
              <Stat
                icon={Plug}
                label="Connections"
                value={num(stats.server.connections)}
              />
              <Stat
                icon={Layers}
                label="Subscriptions"
                value={num(stats.server.subscriptions)}
              />
              <Stat
                icon={ArrowDownToLine}
                label="Messages in"
                value={num(stats.server.inMsgs)}
              />
              <Stat
                icon={ArrowUpFromLine}
                label="Messages out"
                value={num(stats.server.outMsgs)}
              />
              <Stat
                icon={Download}
                label="Data in"
                value={bytes(stats.server.inBytes)}
              />
              <Stat
                icon={Upload}
                label="Data out"
                value={bytes(stats.server.outBytes)}
              />
              <Stat
                icon={History}
                label="Total connections"
                value={num(stats.server.totalConnections)}
              />
              <Stat
                icon={TriangleAlert}
                label="Slow consumers"
                value={num(stats.server.slowConsumers)}
                alert={stats.server.slowConsumers > 0}
              />
            </div>

            <h2 className="mt-10 text-sm font-semibold uppercase tracking-wide text-zinc-400">
              Destinations
            </h2>
            <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
              Subjects clients consume from. Expand one to see who consumes it
              and how much of it each has taken.
            </p>
            <div className="mt-4">
              {stats.destinations.length === 0 ? (
                <EmptyState
                  icon={Network}
                  title="No active destinations"
                  body="No clients are subscribed to any queues right now."
                />
              ) : (
                <QueueDestinations destinations={stats.destinations} />
              )}
            </div>

            <h2 className="mt-10 text-sm font-semibold uppercase tracking-wide text-zinc-400">
              Connections
            </h2>
            <p className="mt-1 text-xs text-zinc-500 dark:text-zinc-400">
              Each open client once, with the traffic that belongs to the client
              rather than to any one subject. Counted from the broker&apos;s
              side, so a client that only consumes publishes nothing — the
              messages it received were published by another connection here.
            </p>
            <div className="mt-4">
              {stats.connections.length === 0 ? (
                <p className="text-sm text-zinc-400">
                  No clients are connected right now.
                </p>
              ) : (
                <ConnectionsTable connections={stats.connections} />
              )}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
