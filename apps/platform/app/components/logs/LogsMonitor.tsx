"use client";

import { ScrollText } from "lucide-react";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import LogsFilters from "./LogsFilters";
import LogsTable from "./LogsTable";
import { useLogStream } from "./useLogStream";

/**
 * The platform logs view: a filter bar over a chronological (oldest-at-top) table
 * of stored log events inside a scrollable container. Filters are mirrored to the
 * URL so a view is bookmarkable. "Load older" pages older rows in at the top;
 * "Tail" polls for new rows, appends them at the bottom, and auto-scrolls to follow
 * (only while already at the bottom, so reading back isn't interrupted). Read-only;
 * a failed/unconfigured query shows the unavailable state. The data lifecycle lives
 * in useLogStream; this component only renders.
 */
export default function LogsMonitor() {
  const {
    filters,
    setFilters,
    entries,
    apps,
    olderCursor,
    error,
    loading,
    loadingMore,
    tailing,
    toggleTail,
    loadMore,
    scrollRef,
    onScroll,
  } = useLogStream();

  const isEmpty = entries.length === 0;

  return (
    <div className="flex min-h-0 flex-1 flex-col px-6 py-6">
      <h1 className="text-xl font-semibold tracking-tight">Logs</h1>
      <p className="mt-1 text-sm text-zinc-500 dark:text-zinc-400">
        {tailing
          ? "Tailing live — new events appear at the bottom."
          : "Log events shipped by your running deployments."}
      </p>

      <div className="mt-5">
        <LogsFilters
          value={filters}
          apps={apps}
          onChange={setFilters}
          tailing={tailing}
          onToggleTail={toggleTail}
        />
      </div>

      {error && (
        <p className="mt-4 rounded-lg border border-red-500/20 bg-red-500/5 px-3 py-2 text-sm text-red-500">
          {error}
        </p>
      )}

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="mt-4 min-h-0 flex-1 overflow-y-auto rounded-xl border border-black/10 dark:border-white/10"
      >
        {loading && isEmpty ? (
          <p className="p-4 text-sm text-zinc-400">Loading logs…</p>
        ) : isEmpty ? (
          <div className="p-6">
            <EmptyState
              icon={ScrollText}
              title={error ? "Logs unavailable" : "No logs found"}
              body={
                error
                  ? "The platform can't reach the observability service. Set OBSERVABILITY_URL to enable it."
                  : "No log events match these filters yet."
              }
            />
          </div>
        ) : (
          <>
            {olderCursor && (
              <div className="flex justify-center border-b border-black/5 p-2 dark:border-white/5">
                <button
                  type="button"
                  onClick={loadMore}
                  disabled={loadingMore}
                  className="rounded-md px-3 py-1 text-xs text-zinc-500 transition-colors hover:bg-black/[0.05] disabled:opacity-50 dark:hover:bg-white/[0.06]"
                >
                  {loadingMore ? "Loading…" : "Load older"}
                </button>
              </div>
            )}
            <LogsTable entries={entries} />
          </>
        )}
      </div>
    </div>
  );
}
