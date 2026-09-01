"use client";

import { useState } from "react";
import { Check, ChevronDown, ChevronRight, Loader2, ShieldQuestion, Wrench, X } from "lucide-react";
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

export default function ToolChip({
  run,
  onAuthorize,
}: {
  run: ToolRun;
  /** Answer this call, when it is one the agent is holding for a person. */
  onAuthorize: (id: string, allow: boolean) => void;
}) {
  const asking = run.authorization?.state === "pending";
  const [open, setOpen] = useState(asking);

  // Open it when the question ARRIVES, not only when the chip happens to mount
  // holding one. The agent reports a call before it asks about it, so this chip is
  // almost always already on screen and closed by the time the question lands —
  // and a person cannot decide about a call whose arguments are behind a click.
  //
  // Adjusted during render rather than in an effect: React re-runs this component
  // before touching the DOM, so the chip is never painted asking-but-closed. It
  // stays a nudge and not a lock — the header still collapses it afterwards, which
  // matters for the one chip somebody has decided to leave open and ignore.
  const [asked, setAsked] = useState(asking);
  if (asking !== asked) {
    setAsked(asking);
    if (asking) setOpen(true);
  }
  const detail = preview(run.input);
  const result = preview(run.output);
  const expandable = Boolean(detail || result);
  const summary = summarise(run.input);

  const tone = asking
    ? "bg-amber-500/10 text-amber-700 dark:text-amber-400 border-amber-500/30"
    : run.failed
    ? "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/20"
    : run.done
      ? "bg-black/[0.04] text-zinc-600 border-black/[0.08] dark:bg-white/[0.06] dark:text-zinc-300 dark:border-white/10"
      : "bg-sky-500/10 text-sky-600 dark:text-sky-400 border-sky-500/20";

  const Status = asking ? ShieldQuestion : run.failed ? X : run.done ? Check : Loader2;
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
          className={`ml-auto shrink-0 ${run.done || run.failed || asking ? "" : "animate-spin"}`}
        />
      </button>

      {open && (
        <div className="border-t border-current/10 px-2 py-1.5">
          {detail && <Block label="Arguments" body={detail} />}
          {result && <Block label={run.failed ? "Error" : "Result"} body={result} />}
          {asking && (
            <Ask
              waiting={run.authorization?.expiresInSeconds}
              onAnswer={(allow) => onAuthorize(run.authorization!.id, allow)}
            />
          )}
        </div>
      )}
    </div>
  );
}

/**
 * The question, under the arguments it is about.
 *
 * Nothing here is optimistic: the buttons send the answer and leave the chip as it
 * is. What happened to the call comes back on the run's own stream — the runtime
 * reports the decision it acted on — so a click that did not land shows as the
 * denial the run will eventually make, rather than as an approval this panel
 * invented.
 */
function Ask({
  waiting,
  onAnswer,
}: {
  waiting?: number;
  onAnswer: (allow: boolean) => void;
}) {
  return (
    <div className="mt-1.5 flex items-center gap-1.5 border-t border-current/10 pt-1.5">
      <span className="mr-auto text-[10px] opacity-70">
        Needs your say-so
        {waiting ? ` — denied on its own in ${Math.round(waiting / 60)} min` : ""}
      </span>
      <button
        type="button"
        onClick={() => onAnswer(false)}
        className="rounded border border-current/20 px-1.5 py-0.5 text-[10px] font-medium hover:bg-black/[0.06] dark:hover:bg-white/10"
      >
        Deny
      </button>
      <button
        type="button"
        onClick={() => onAnswer(true)}
        className="rounded bg-amber-600 px-1.5 py-0.5 text-[10px] font-medium text-white hover:bg-amber-700"
      >
        Allow
      </button>
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
