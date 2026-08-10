"use client";

import { GitBranch, Sparkles } from "lucide-react";
import type { TraceSummary } from "@/app/model/traces";
import { describeCost, formatAge, formatDuration } from "./format";

/**
 * One app's traces, newest first.
 *
 * The duration shown is the trace's whole span — `endedAt - startedAt` — not the
 * root flow's own measured duration. The span exists for every trace, including
 * one whose terminal record was lost, and it is what the waterfall draws; using
 * the root's duration instead would give a trace two different lengths depending
 * on which pane you read it in.
 */
export default function TraceList({
  traces,
  selectedId,
  loading,
  loadingMore,
  hasMore,
  onSelect,
  onLoadMore,
}: {
  traces: TraceSummary[];
  selectedId: string | null;
  loading: boolean;
  loadingMore: boolean;
  hasMore: boolean;
  onSelect: (trace: TraceSummary) => void;
  onLoadMore: () => void;
}) {
  if (loading && traces.length === 0) {
    return <p className="px-3 py-4 text-xs text-zinc-400">Loading traces…</p>;
  }
  if (traces.length === 0) {
    return (
      <p className="px-3 py-4 text-xs text-zinc-400">
        No trace matches these filters in this window.
      </p>
    );
  }

  return (
    <>
      <ul className="divide-y divide-black/[0.04] dark:divide-white/[0.06]">
        {traces.map((trace) => (
          <li key={trace.traceId}>
            <TraceRow
              trace={trace}
              selected={trace.traceId === selectedId}
              onSelect={() => onSelect(trace)}
            />
          </li>
        ))}
      </ul>

      {hasMore && (
        <div className="p-2">
          <button
            type="button"
            onClick={onLoadMore}
            disabled={loadingMore}
            className="w-full rounded-md border border-black/10 px-3 py-1.5 text-xs font-medium text-zinc-600 transition-colors hover:bg-black/[0.04] disabled:opacity-50 dark:border-white/15 dark:text-zinc-300 dark:hover:bg-white/[0.06]"
          >
            {loadingMore ? "Loading…" : "Load older"}
          </button>
        </div>
      )}
    </>
  );
}

/** The dot that carries a trace's outcome, since the row has no room for a word. */
const STATUS_DOT: Record<string, string> = {
  ok: "bg-emerald-500",
  failed: "bg-red-500",
  dropped: "bg-amber-500",
};

function TraceRow({
  trace,
  selected,
  onSelect,
}: {
  trace: TraceSummary;
  selected: boolean;
  onSelect: () => void;
}) {
  const spanNs = (Date.parse(trace.endedAt) - Date.parse(trace.startedAt)) * 1_000_000;
  const cost = describeCost(trace.costUsd, trace.unpricedCalls, trace.llmCalls > 0);
  const crossed = trace.deploymentIds.length > 1;

  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={selected ? "true" : undefined}
      className={`flex w-full flex-col gap-1 px-3 py-2 text-left transition-colors ${
        selected ? "bg-sky-500/10" : "hover:bg-black/[0.03] dark:hover:bg-white/[0.04]"
      }`}
    >
      <div className="flex items-center gap-2">
        <span
          // A bare span's aria-label is not exposed by many screen readers, so
          // without a role the outcome of a trace is carried by colour alone.
          role="img"
          aria-label={trace.status}
          title={trace.status}
          className={`size-2 shrink-0 rounded-full ${STATUS_DOT[trace.status] ?? "bg-zinc-400"}`}
        />
        <span className="min-w-0 flex-1 truncate text-sm">
          {trace.entryLabel || trace.rootFlow || trace.traceId}
        </span>
        <span className="shrink-0 font-mono text-xs text-zinc-500 dark:text-zinc-400">
          {formatDuration(spanNs)}
        </span>
      </div>

      <div className="flex items-center gap-2 pl-4 text-xs text-zinc-500 dark:text-zinc-400">
        {trace.rootFlow && trace.entryLabel && (
          <span className="truncate">{trace.rootFlow}</span>
        )}
        {trace.llmCalls > 0 && (
          <span
            title={
              trace.models.length > 0 ? trace.models.join(", ") : "Model calls in this trace"
            }
            className="inline-flex shrink-0 items-center gap-1 text-violet-600 dark:text-violet-400"
          >
            <Sparkles size={11} />
            {trace.llmCalls}
          </span>
        )}
        <span
          title={cost.title}
          className={`shrink-0 ${cost.partial ? "text-amber-600 dark:text-amber-400" : ""}`}
        >
          {cost.text}
        </span>
        {crossed && (
          <span
            title={`This trace crossed ${trace.deploymentIds.length} deployments — the trace id rides on the message and survives a queue hop.`}
            className="inline-flex shrink-0 items-center gap-1"
          >
            <GitBranch size={11} />
            {trace.deploymentIds.length}
          </span>
        )}
        <span className="ml-auto shrink-0">{formatAge(trace.startedAt)}</span>
      </div>
    </button>
  );
}
