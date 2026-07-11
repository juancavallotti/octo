"use client";

import { CircleSlash, Filter, Play, SquareDashedBottom } from "lucide-react";
import type { FlowRunEntry, FlowRunStatus } from "../../run/FlowRunContext";

/**
 * What the flows produced. Results are *output* — the message a run returned, or the
 * message it was carrying at a breakpoint — never log lines: those are the Logs tab's,
 * and a manual run is quiet by design.
 *
 * The three "nothing came back" outcomes are given their own words rather than being
 * lumped together as a failure, because they mean different things and the difference
 * is exactly what the user is trying to find out.
 */

const STATUS: Record<FlowRunStatus, { label: string; className: string }> = {
  running: {
    label: "running",
    className: "bg-black/[0.06] text-zinc-500 dark:bg-white/10 dark:text-zinc-400",
  },
  ok: {
    label: "ok",
    className: "bg-emerald-500/15 text-emerald-600 dark:text-emerald-400",
  },
  dropped: {
    label: "dropped",
    className: "bg-amber-500/15 text-amber-600 dark:text-amber-400",
  },
  "not-reached": {
    label: "not reached",
    className: "bg-sky-500/15 text-sky-600 dark:text-sky-400",
  },
  error: { label: "error", className: "bg-rose-500/15 text-rose-600 dark:text-rose-400" },
  timeout: {
    label: "timed out",
    className: "bg-rose-500/15 text-rose-600 dark:text-rose-400",
  },
};

/** The line explaining an outcome that produced no output. */
function emptyNote(entry: FlowRunEntry): { icon: typeof Filter; text: string } | null {
  switch (entry.status) {
    case "dropped":
      return {
        icon: Filter,
        text: "The flow filtered this message — a block returned nothing, so there is no result.",
      };
    case "not-reached":
      return {
        icon: CircleSlash,
        text: "The flow never reached this block: it took another branch. That is a result, not a failure.",
      };
    default:
      return null;
  }
}

function Entry({ entry }: { entry: FlowRunEntry }) {
  const status = STATUS[entry.status];
  const note = emptyNote(entry);

  return (
    <li className="border-b border-black/5 px-3 py-2 last:border-0 dark:border-white/5">
      <div className="flex items-center gap-2">
        <span
          className={`rounded-full px-1.5 py-0.5 text-[10px] font-medium leading-none ${status.className}`}
        >
          {status.label}
        </span>
        <span className="font-mono text-xs text-zinc-700 dark:text-zinc-300">
          {entry.flowName}
        </span>
        {entry.breakpointLabel && (
          <span className="flex items-center gap-1 text-[11px] text-zinc-500 dark:text-zinc-400">
            <SquareDashedBottom size={12} aria-hidden />
            broke at {entry.breakpointLabel}
          </span>
        )}
        {entry.inputName && (
          <span className="text-[11px] text-zinc-400 dark:text-zinc-500">
            {entry.inputName}
          </span>
        )}
      </div>

      {entry.error && (
        <pre className="mt-1 whitespace-pre-wrap break-words font-mono text-[11px] text-rose-600 dark:text-rose-400">
          {entry.error}
        </pre>
      )}
      {note && (
        <p className="mt-1 flex items-start gap-1.5 text-[11px] text-zinc-500 dark:text-zinc-400">
          <note.icon size={12} className="mt-0.5 shrink-0" aria-hidden />
          {note.text}
        </p>
      )}
      {entry.output !== undefined && (
        <pre className="mt-1 overflow-x-auto whitespace-pre-wrap break-words rounded bg-black/[0.03] p-2 font-mono text-[11px] leading-relaxed text-zinc-700 dark:bg-white/[0.04] dark:text-zinc-300">
          {JSON.stringify(entry.output, null, 2)}
        </pre>
      )}
    </li>
  );
}

export default function ResultsTab({ results }: { results: FlowRunEntry[] }) {
  if (results.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center gap-2 px-3 text-center text-xs text-zinc-400 dark:text-zinc-600">
        <Play size={13} />
        Run a flow from its play button to see what it produces.
      </div>
    );
  }

  return (
    <ul className="flex-1 overflow-auto">
      {results.map((entry) => (
        <Entry key={entry.id} entry={entry} />
      ))}
    </ul>
  );
}
