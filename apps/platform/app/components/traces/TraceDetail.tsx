"use client";

import { useState } from "react";
import { Waypoints } from "lucide-react";
import { EmptyState } from "@/app/(session)/platform/DashboardTiles";
import { formatDuration } from "./format";
import type { WaterfallNode } from "./types";
import RecordInspector from "./RecordInspector";
import TraceSummaryPanel from "./TraceSummaryPanel";
import { useTraceDetail } from "./useTraceDetail";
import Waterfall from "./Waterfall";

/**
 * The right-hand pane: one trace, as the waterfall of everything that happened
 * on it.
 *
 * The heading reads the *stored* rollup rather than anything recomputed from the
 * records below it, which is what makes the number here and the number in the
 * list the same number instead of two implementations that agree until one of
 * them changes.
 */
export default function TraceDetail({ traceId }: { traceId: string }) {
  const { detail, waterfall, loading, error } = useTraceDetail(traceId);
  const [selected, setSelected] = useState<WaterfallNode | null>(null);

  if (loading) {
    return <p className="p-6 text-sm text-zinc-400">Loading trace…</p>;
  }
  if (error || !detail || !waterfall) {
    return (
      <div className="p-6">
        <EmptyState
          icon={Waypoints}
          title="Trace unavailable"
          body={error ?? "This trace could not be loaded."}
        />
      </div>
    );
  }

  const { summary } = detail;
  const spanNs = (Date.parse(summary.endedAt) - Date.parse(summary.startedAt)) * 1e6;

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="border-b border-black/10 px-4 py-3 dark:border-white/10">
        <div className="flex items-baseline gap-2">
          <h2 className="min-w-0 flex-1 truncate text-sm font-semibold">
            {summary.entryLabel || summary.rootFlow || summary.traceId}
          </h2>
          <span className="shrink-0 font-mono text-sm">{formatDuration(spanNs)}</span>
        </div>
        <p className="mt-0.5 truncate font-mono text-[11px] text-zinc-400">
          {summary.traceId}
        </p>
      </header>

      {detail.truncated && (
        <p className="border-b border-amber-500/20 bg-amber-500/5 px-4 py-2 text-xs text-amber-600 dark:text-amber-400">
          This trace holds more records than one response carries, so the chart
          below is drawn from part of it. A waterfall cut this way is incomplete in
          a way the picture itself cannot show.
        </p>
      )}

      <TraceSummaryPanel summary={summary} waterfall={waterfall} />

      <div className="flex min-h-0 flex-1 flex-col overflow-hidden">
        {/* Keyed by the trace, so its viewport and folded branches start fresh
            rather than carrying one trace's shape onto another. */}
        <Waterfall
          key={summary.traceId}
          waterfall={waterfall}
          selectedId={selected?.id ?? null}
          onSelect={setSelected}
        />
      </div>

      {selected && (
        // Keyed by the span, so opening another one refetches rather than
        // showing the previous record's payloads under the new heading.
        <RecordInspector
          key={selected.id}
          traceId={summary.traceId}
          node={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}
