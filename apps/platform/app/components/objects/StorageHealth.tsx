"use client";

import { useCallback, useEffect, useState } from "react";
import { RefreshCw, TriangleAlert } from "lucide-react";
import { getStorageStats, type StorageStats } from "@/app/model/storage";
import { DatabaseTiles, RedisTiles } from "./StorageTiles";

/** How often to re-poll. Same cadence as the broker monitor; these are counters. */
const POLL_MS = 5000;

/**
 * How full the two stores behind the object browser are.
 *
 * It lives beside the objects rather than on its own page because the questions
 * arrive together: someone looking at a volatile object that is not there wants to
 * know whether Redis evicted it, and the eviction count is the answer. The
 * platform-services page next door reports only whether each store answered a ping;
 * this is the part a ping cannot tell you.
 *
 * Either store may be absent, and absent is not broken — an installation with no
 * Redis is supported, and volatile objects simply fall back to the database. The
 * reason string says which, because showing them the same way would send someone
 * looking for a fault that is not there.
 */
export default function StorageHealth() {
  const [stats, setStats] = useState<StorageStats | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  const load = useCallback(
    () =>
      getStorageStats().then(
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

  return (
    <div className="flex flex-col gap-6 p-4">
      <div className="flex items-center justify-between">
        <p className="text-sm text-zinc-500 dark:text-zinc-400">
          Live counters for the two stores behind this page, re-read every few
          seconds.
        </p>
        <button
          type="button"
          onClick={refresh}
          className="flex items-center gap-1.5 rounded-lg border border-black/10 px-2.5 py-1.5 text-xs text-zinc-600 hover:bg-black/5 dark:border-white/10 dark:text-zinc-300 dark:hover:bg-white/5"
        >
          <RefreshCw size={13} className={refreshing ? "animate-spin" : ""} />
          Refresh
        </button>
      </div>

      {error ? (
        <div className="flex items-center gap-2 rounded-lg border border-red-500/30 bg-red-500/5 px-3 py-2 text-sm text-red-600 dark:text-red-400">
          <TriangleAlert size={14} aria-hidden />
          {error}
        </div>
      ) : null}

      <Section
        title="Volatile tier (Redis)"
        note="Where cache entries and volatile objects live. Values here may be evicted under memory pressure — that is what volatile means, not a fault."
        reason={stats?.redisReason}
      >
        {stats?.redis ? <RedisTiles stats={stats.redis} /> : null}
      </Section>

      <Section
        title="Persistent tier (database)"
        note="Where everything that has to survive a restart lives, including secrets."
        reason={stats?.databaseReason}
      >
        {stats?.database ? <DatabaseTiles stats={stats.database} /> : null}
      </Section>
    </div>
  );
}

/** One store's block: a heading, a note about what it holds, and its tiles. */
function Section({
  title,
  note,
  reason,
  children,
}: {
  title: string;
  note: string;
  /** Why this store reported nothing, when it reported nothing. */
  reason?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="flex flex-col gap-3">
      <div>
        <h2 className="text-sm font-semibold">{title}</h2>
        <p className="mt-0.5 text-xs text-zinc-500 dark:text-zinc-400">{note}</p>
      </div>
      {reason ? (
        <div className="rounded-lg border border-black/10 bg-white/40 px-3 py-2 text-sm text-zinc-500 dark:border-white/10 dark:bg-zinc-900/30 dark:text-zinc-400">
          {reason}
        </div>
      ) : (
        children
      )}
    </section>
  );
}
