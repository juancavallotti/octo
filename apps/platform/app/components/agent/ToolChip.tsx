"use client";

import { useState } from "react";
import { Check, ChevronDown, ChevronRight, Loader2, Wrench, X } from "lucide-react";
import type { ToolRun } from "./useAgentChat";

/**
 * One tool call, as a chip that opens.
 *
 * Closed it says what ran and whether it worked; open it shows the arguments the
 * model chose and what came back. That second half is the point — it is the
 * difference between believing an answer and being able to check it, and for an
 * agent with write access to the platform it is also the audit trail you have
 * without turning tracing on.
 */

/** Render a tool's arguments or result compactly, whatever shape they arrive in. */
function preview(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    return String(value);
  }
}

/** The one line worth showing before anything is expanded. */
function summarise(input: unknown): string {
  if (!input || typeof input !== "object") return "";
  const args = input as Record<string, unknown>;
  // method+path reads as the request it is, which is what octo_api mostly does.
  if (typeof args.method === "string" && typeof args.path === "string") {
    return `${args.method} ${args.path}`;
  }
  for (const key of ["path", "tag", "name", "id", "query"]) {
    if (typeof args[key] === "string" && args[key]) return args[key] as string;
  }
  return "";
}

export default function ToolChip({ run }: { run: ToolRun }) {
  const [open, setOpen] = useState(false);
  const detail = preview(run.input);
  const result = preview(run.output);
  const expandable = Boolean(detail || result);
  const summary = summarise(run.input);

  const tone = run.failed
    ? "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/20"
    : run.done
      ? "bg-black/[0.04] text-zinc-600 border-black/[0.08] dark:bg-white/[0.06] dark:text-zinc-300 dark:border-white/10"
      : "bg-sky-500/10 text-sky-600 dark:text-sky-400 border-sky-500/20";

  const Status = run.failed ? X : run.done ? Check : Loader2;
  const Chevron = open ? ChevronDown : ChevronRight;

  return (
    <div className={`overflow-hidden rounded-md border text-[11px] ${tone}`}>
      <button
        type="button"
        disabled={!expandable}
        onClick={() => setOpen((v) => !v)}
        aria-expanded={expandable ? open : undefined}
        className="flex w-full items-center gap-1.5 px-2 py-1 text-left disabled:cursor-default"
      >
        {expandable ? (
          <Chevron size={11} className="shrink-0 opacity-60" />
        ) : (
          <Wrench size={11} className="shrink-0 opacity-60" />
        )}
        <span className="font-mono font-medium">{run.tool}</span>
        {summary && (
          <span className="min-w-0 flex-1 truncate font-mono opacity-70">{summary}</span>
        )}
        <Status
          size={11}
          className={`ml-auto shrink-0 ${run.done || run.failed ? "" : "animate-spin"}`}
        />
      </button>

      {open && (
        <div className="border-t border-current/10 px-2 py-1.5">
          {detail && <Block label="Arguments" body={detail} />}
          {result && <Block label={run.failed ? "Error" : "Result"} body={result} />}
        </div>
      )}
    </div>
  );
}

function Block({ label, body }: { label: string; body: string }) {
  return (
    <div className="mb-1.5 last:mb-0">
      <div className="mb-0.5 text-[10px] font-medium uppercase opacity-50">{label}</div>
      {/* Capped and scrollable: a list of every integration should not push the
          conversation off the screen. */}
      <pre className="max-h-32 overflow-auto rounded bg-black/[0.04] p-1.5 font-mono text-[10px] leading-snug whitespace-pre-wrap dark:bg-black/20">
        {body}
      </pre>
    </div>
  );
}
