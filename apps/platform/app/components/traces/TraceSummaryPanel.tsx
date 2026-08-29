"use client";

import { useMemo } from "react";
import type { TraceSummary } from "@/app/model/traces";
import { workBreakdown } from "./blockClasses";
import { describeCost, formatDuration, formatTokens } from "./format";
import type { Waterfall } from "./types";
import WorkSplit from "./WorkSplit";

/**
 * What the trace adds up to.
 *
 * The split is deliberate. Every **aggregate** here — status, duration, tokens,
 * cost, models — comes from the stored rollup rather than being recomputed from
 * the records, so the number in the trace list and the number in this panel are
 * the same number instead of two implementations that agree until one changes.
 *
 * The **I/O-vs-CPU breakdown** is the one thing computed here, because it needs
 * the interval arithmetic the waterfall just did, and because which blocks count
 * as waiting is presentation policy that changes whenever a connector is added.
 */
export default function TraceSummaryPanel({
  summary,
  waterfall,
}: {
  summary: TraceSummary;
  waterfall: Waterfall;
}) {
  const breakdown = useMemo(() => workBreakdown(waterfall), [waterfall]);
  // From the rollup, like every other aggregate here. Reading it off the chart
  // would make this the one number in the panel that a truncated record set
  // could quietly shrink, in the panel whose whole claim is that it does not
  // recompute what the list already showed.
  const spanNs = (Date.parse(summary.endedAt) - Date.parse(summary.startedAt)) * 1e6;
  const cost = describeCost(summary.costUsd, summary.unpricedCalls, summary.llmCalls > 0);

  return (
    <section className="grid gap-4 border-b border-black/10 px-4 py-3 md:grid-cols-2 dark:border-white/10">
      <div className="space-y-1.5">
        <h3 className="text-[11px] font-semibold uppercase tracking-wide text-zinc-500">
          Where the time went
        </h3>
        <WorkSplit breakdown={breakdown} />
      </div>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 text-xs sm:grid-cols-3">
        <Stat label="Status" title={`This trace ended ${summary.status}`}>
          <span
            className={
              summary.status === "failed"
                ? "text-red-500"
                : summary.status === "dropped"
                  ? "text-amber-600 dark:text-amber-400"
                  : "text-emerald-600 dark:text-emerald-400"
            }
          >
            {summary.status}
          </span>
        </Stat>

        <Stat
          label="Records"
          title="How many records were rolled up for this trace, which may be more than the chart draws."
        >
          {summary.records}
        </Stat>

        <Stat
          label="Root flow"
          title="The flow the trace started in. A flow-ref, split or aggregate runs inside it as its own invocation."
        >
          <span className="truncate">{summary.rootFlow || "—"}</span>
        </Stat>

        {summary.llmCalls > 0 && (
          <>
            {/* The models are on the surface rather than in the tooltip they
                used to hide in. "Which model was this" is one of the first
                questions asked of a trace that cost more than expected, and a
                title attribute is invisible on touch and to a screen reader —
                the same argument the token split is already split out for. */}
            <Stat
              label="Model calls"
              detail={summary.models.join(" · ")}
              title={summary.models.join(", ")}
            >
              {summary.llmCalls}
            </Stat>

            <TokenStat summary={summary} />

            <Stat label="Cost" title={cost.title}>
              <span className={cost.partial ? "text-amber-600 dark:text-amber-400" : ""}>
                {cost.text}
              </span>
            </Stat>
          </>
        )}

        {summary.deploymentIds.length > 1 && (
          <Stat
            label="Deployments"
            title="This trace crossed more than one deployment — the trace id rides on the message and survives a queue hop, so this is not one app's story."
          >
            {summary.deploymentIds.length}
          </Stat>
        )}

        <Stat label="Span" title="End to end, as the stored rollup measured it.">
          <span className="font-mono">{formatDuration(spanNs)}</span>
        </Stat>
      </dl>
    </section>
  );
}

/**
 * The token split, shown rather than hidden.
 *
 * It used to be a `title` on the total, which is to say it was invisible on a
 * touch device, absent from a screen reader, and undiscoverable everywhere else.
 * The decomposition is the number people actually reason about — a trace that is
 * mostly input is a prompt problem, one that is mostly output is a generation
 * problem — so it is on the page.
 *
 * The total keeps the position it had, because the trace list and the app list
 * still lead with a single figure and moving between them should not mean
 * re-finding it.
 */
function TokenStat({ summary }: { summary: TraceSummary }) {
  return (
    <Stat
      label="Tokens"
      // The one fact the numbers cannot carry: they are not addends.
      title="Output already includes any thinking tokens — the provider counts them there, so the two are never added."
      detail={
        <>
          {formatTokens(summary.inputTokens)} in · {formatTokens(summary.outputTokens)} out
          {summary.cachedTokens > 0 && <> · {formatTokens(summary.cachedTokens)} cached</>}
        </>
      }
    >
      {formatTokens(summary.inputTokens + summary.outputTokens)}
    </Stat>
  );
}

function Stat({
  label,
  title,
  detail,
  children,
}: {
  label: string;
  title?: string;
  /**
   * A second line under the value, for a breakdown the value alone cannot carry.
   * It wraps rather than truncating: a decomposition cut off mid-way reads as a
   * different, smaller number, which is worse than taking a second line.
   */
  detail?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="min-w-0" title={title}>
      <dt className="text-[10px] uppercase tracking-wide text-zinc-400">{label}</dt>
      <dd className="truncate">{children}</dd>
      {detail && (
        <dd className="text-[11px] leading-tight text-zinc-500 dark:text-zinc-400">{detail}</dd>
      )}
    </div>
  );
}
