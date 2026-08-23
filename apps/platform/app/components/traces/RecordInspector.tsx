"use client";

import { X } from "lucide-react";
import { bodyIsFirstOnly, foldedCount } from "./folded";
import { classifySpan } from "./blockClasses";
import { describeCost, describeCostStatus, formatDuration, formatTokens } from "./format";
import Payload from "./Payload";
import type { WaterfallNode } from "./types";
import { useTraceRecord } from "./useTraceRecord";

/**
 * One span, opened.
 *
 * The trace was fetched without payloads, so the body and variables are pulled
 * here — for this record, and for the `block.pre-invoke` that carried what the
 * block was handed, when that one could be attributed. When it could not, this
 * says so rather than showing a plausible input: under a `fork` the runtime is
 * explicit that pre/post pairing is unreliable, and a wrong payload shown
 * confidently is worse than none, because nothing about it looks wrong.
 */
export default function RecordInspector({
  traceId,
  node,
  onClose,
}: {
  traceId: string;
  node: WaterfallNode;
  onClose: () => void;
}) {
  const output = useTraceRecord(traceId, node.record?.id ?? null);
  const input = useTraceRecord(traceId, node.input?.id ?? null);
  const record = output.record ?? node.record;

  return (
    <aside className="flex max-h-[45%] min-h-0 shrink-0 flex-col border-t border-black/10 dark:border-white/10">
      <header className="flex items-center gap-2 border-b border-black/10 px-4 py-2 dark:border-white/10">
        <span className="truncate text-sm font-medium">{node.label}</span>
        <span className="shrink-0 rounded bg-black/[0.06] px-1.5 py-0.5 font-mono text-[10px] text-zinc-500 dark:bg-white/[0.08] dark:text-zinc-400">
          {node.kind}
        </span>
        {!node.inferred && (
          <span className="shrink-0 font-mono text-xs text-zinc-500 dark:text-zinc-400">
            {formatDuration(node.durationNs)}
          </span>
        )}
        <button
          type="button"
          aria-label="Close"
          onClick={onClose}
          className="ml-auto shrink-0 rounded p-1 text-zinc-400 transition-colors hover:bg-black/[0.06] hover:text-zinc-700 dark:hover:bg-white/[0.08] dark:hover:text-zinc-200"
        >
          <X size={14} />
        </button>
      </header>

      <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-3">
        {node.path && (
          <p
            title="The block's address in its flow — the same grammar `octo invoke --spies` accepts."
            className="break-all font-mono text-[11px] text-zinc-500 dark:text-zinc-400"
          >
            {node.path}
            {record?.blockType ? ` · ${record.blockType}` : ""}
            {` · ${classifySpan(node)}`}
          </p>
        )}

        {record?.error && (
          <p className="rounded-md border border-red-500/20 bg-red-500/5 px-3 py-2 text-xs text-red-500">
            {record.error}
          </p>
        )}

        {record && foldedCount(record) > 0 && (
          <p className="rounded-md border border-sky-500/20 bg-sky-500/5 px-3 py-2 text-xs text-sky-600 dark:text-sky-400">
            This row is {foldedCount(record)} records the aggregator collapsed into
            one — a streaming block emits a pair per frame, and an agent&rsquo;s
            answer is one frame per token.{" "}
            {bodyIsFirstOnly(record)
              ? "Their payloads could not be merged, so the body below is the first one alone."
              : "The body below is their payloads joined back together, in order."}
          </p>
        )}

        {node.inferred && (
          <p className="rounded-md border border-amber-500/20 bg-amber-500/5 px-3 py-2 text-xs text-amber-600 dark:text-amber-400">
            No record ended this invocation, so its extent is inferred from what
            ran inside it — a lower bound on what really happened here.
          </p>
        )}

        {record && record.costStatus !== "" && <ModelCall record={record} />}

        {record?.attrs && Object.keys(record.attrs).length > 0 && (
          <Payload
            label="Attributes"
            value={record.attrs}
            absentNote="No attributes."
          />
        )}

        <Payload
          label="Input"
          value={input.record?.body ?? null}
          loading={input.loading}
          absentNote={
            node.inputMatched
              ? node.input
                ? "This block recorded no input payload."
                : "No input was recorded for this block."
              : "Input not matched — this address ran concurrently with itself, so nothing here can say which input became this outcome."
          }
        />

        <Payload
          label="Output"
          value={record?.body ?? null}
          loading={output.loading}
          absentNote="No payload was captured. Body capture can be turned off per deployment."
        />

        <Payload
          label="Variables"
          value={record?.vars ?? null}
          loading={output.loading}
          absentNote="No variables were captured."
        />
      </div>
    </aside>
  );
}

/** The billing side of a model call, and how far pricing actually got. */
function ModelCall({ record }: { record: NonNullable<WaterfallNode["record"]> }) {
  const cost = describeCost(record.costUsd ?? 0, record.costUsd === null ? 1 : 0, true);
  const why = describeCostStatus(record.costStatus);

  return (
    <div className="space-y-1 rounded-md border border-black/10 px-3 py-2 text-xs dark:border-white/10">
      <div className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
        <span className="font-mono">{record.model || "(model not reported)"}</span>
        <span className={cost.partial ? "text-amber-600 dark:text-amber-400" : ""}>
          {cost.text}
        </span>
      </div>
      <p className="text-zinc-500 dark:text-zinc-400">
        {tokens(record)}
      </p>
      {why && <p className="text-zinc-400">{why}</p>}
    </div>
  );
}

/** Token counts, naming the two rules that make them easy to add up wrongly. */
function tokens(record: NonNullable<WaterfallNode["record"]>): string {
  const parts: string[] = [];
  if (record.inputTokens !== null) parts.push(`${formatTokens(record.inputTokens)} in`);
  if (record.outputTokens !== null) parts.push(`${formatTokens(record.outputTokens)} out`);
  // Already inside the output count — the provider reports it there, so the two
  // are never summed.
  if (record.thinkingTokens) parts.push(`${formatTokens(record.thinkingTokens)} of it thinking`);
  if (record.cachedTokens) parts.push(`${formatTokens(record.cachedTokens)} cache reads`);
  // Beside the reads, not folded in with them: a write bills above the input
  // rate and a read below it, so one number for both would explain neither.
  if (record.cacheWriteTokens) parts.push(`${formatTokens(record.cacheWriteTokens)} cache writes`);
  return parts.length > 0 ? parts.join(" · ") : "The provider reported no usage.";
}
