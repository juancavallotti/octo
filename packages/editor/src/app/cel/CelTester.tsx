"use client";

import { useMemo, useState } from "react";
import { Play } from "lucide-react";
import CelEditor from "./CelEditor";
import JsonEditor from "./JsonEditor";
import { useRun } from "../run/RunContext";
import { useCelTester, type CelRun } from "./CelTesterStore";
import type { MemberProvider } from "./complete";
import { LIST_METHODS, STRING_METHODS, type CelEntry } from "./catalog";

/**
 * Trying a CEL expression against an ad-hoc input, without running a flow. The
 * expression uses the same highlighted, autocompleting CelEditor; the vars and body
 * inputs are JSON, matching what a message expression sees. Evaluation goes through
 * useRun().evalCel (→ `octo eval`), and each run is kept in a log, so a result can be
 * compared against the one before it instead of replacing it.
 *
 * There is no env box: the console already has a Dev .env tab, and two places to put
 * the same variables is one place too many. `templateResource()` is likewise not
 * available here.
 *
 * It is a console tab rather than the modal it started as. Testing an expression is
 * something you do *while* reading the settings field you are writing it for, and a
 * modal took the document away to do it — the same reason Problems and Logs are down
 * here and not in overlays.
 */

/** Best-effort JSON parse; undefined when empty or invalid (mid-typing). */
function tryParse(text: string): unknown {
  if (text.trim() === "") return undefined;
  try {
    return JSON.parse(text);
  } catch {
    return undefined;
  }
}

/** The CEL type name shown for a sampled member value. */
function jsonType(v: unknown): string {
  if (v === null) return "null";
  if (Array.isArray(v)) return "list";
  switch (typeof v) {
    case "string":
      return "string";
    case "number":
      return "double";
    case "boolean":
      return "bool";
    case "object":
      return "map";
    default:
      return "dyn";
  }
}

/** A short, safe preview of a sampled value for the doc panel. */
function preview(v: unknown): string {
  const s = JSON.stringify(v);
  if (s === undefined) return "";
  return s.length > 40 ? `${s.slice(0, 39)}…` : s;
}

/** Turn a sampled object into member completion entries (name, type, value preview). */
function toEntries(obj: Record<string, unknown>): CelEntry[] {
  return Object.entries(obj).map(([name, v]) => ({
    name,
    kind: "variable" as const,
    signature: jsonType(v),
    summary: `= ${preview(v)}`,
    example: "",
  }));
}

/**
 * A member provider resolving a dotted path (e.g. `body.user`) against the parsed
 * body/vars samples, so `body.` completes with the sample's keys. Returns undefined
 * for unresolvable paths (invalid JSON, non-object leaf) — the menu then offers
 * nothing rather than guessing.
 */
function buildMembers(body: string, vars: string): MemberProvider {
  const roots: Record<string, unknown> = {
    body: tryParse(body),
    vars: tryParse(vars),
  };
  return (path) => {
    let cur = roots[path[0]];
    for (let i = 1; i < path.length; i++) {
      if (cur && typeof cur === "object" && !Array.isArray(cur)) {
        cur = (cur as Record<string, unknown>)[path[i]];
      } else {
        return undefined;
      }
    }
    // A resolved list/string offers its receiver methods; an object offers its keys.
    if (Array.isArray(cur)) return LIST_METHODS;
    if (typeof cur === "string") return STRING_METHODS;
    if (cur && typeof cur === "object") {
      return toEntries(cur as Record<string, unknown>);
    }
    return undefined;
  };
}

/** Validate that optional text is a JSON value (bound to body/vars), or throw. */
function checkJson(text: string, label: string): void {
  if (text.trim() === "") return;
  try {
    JSON.parse(text);
  } catch {
    throw new Error(`${label} must be valid JSON`);
  }
}

export default function CelTester() {
  const run = useRun();
  // The scratchpad outlives the tab (see CelTesterStore); only "a run is in flight"
  // is this component's business.
  const {
    expression,
    setExpression,
    vars,
    setVars,
    body,
    setBody,
    log,
    record,
  } = useCelTester();
  const [busy, setBusy] = useState(false);

  // Member completions for `body.` / `vars.`, derived from the JSON samples.
  const members = useMemo(() => buildMembers(body, vars), [body, vars]);

  const canRun = !busy && expression.trim() !== "" && !!run?.available;

  async function evaluate() {
    if (!run || !canRun) return;
    try {
      checkJson(body, "body");
      checkJson(vars, "vars");
    } catch (e) {
      record({
        expression,
        result: null,
        error: e instanceof Error ? e.message : String(e),
      });
      return;
    }
    setBusy(true);
    try {
      const r = await run.evalCel({
        expression,
        data: body.trim() === "" ? undefined : body,
        vars: vars.trim() === "" ? undefined : vars,
      });
      record({ expression, result: r, error: null });
    } catch (e) {
      record({
        expression,
        result: null,
        error: e instanceof Error ? e.message : String(e),
      });
    } finally {
      setBusy(false);
    }
  }

  // Ctrl/Cmd+Enter runs from anywhere in the tab.
  function onKeyDown(e: React.KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      void evaluate();
    }
  }

  return (
    <div
      onKeyDown={onKeyDown}
      aria-label="CEL expression tester"
      className="flex min-h-0 flex-1 gap-3 overflow-hidden px-3 py-2"
    >
      {/* The sample message on the left, the expression and its answers on the
          right: the console is wide and short, and what you re-read is the log. */}
      <div className="flex w-64 shrink-0 flex-col gap-2 overflow-y-auto">
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-zinc-500">Vars</span>
          <JsonEditor
            value={vars}
            onChange={setVars}
            minHeight={44}
            placeholder={'{ "userId": "u_1" }'}
          />
        </label>
        <label className="flex flex-col gap-1">
          <span className="text-xs font-medium text-zinc-500">Body</span>
          <JsonEditor
            value={body}
            onChange={setBody}
            minHeight={44}
            placeholder={'{ "id": 1 }'}
          />
        </label>
      </div>

      <div className="flex min-w-0 flex-1 flex-col gap-2">
        {/* Centred on the field, not top-aligned: the expression editor grows with
            what is typed into it, and a button pinned to its first line drifts away
            from the box it belongs to. */}
        <div className="flex items-center gap-2">
          <div className="min-w-0 flex-1">
            <CelEditor
              value={expression}
              onChange={setExpression}
              minHeight={36}
              members={members}
            />
          </div>
          <button
            type="button"
            onClick={() => void evaluate()}
            disabled={!canRun}
            title={
              run?.available
                ? "⌘/Ctrl + Enter"
                : "Runner unavailable — start the dev runner to evaluate."
            }
            className="flex shrink-0 items-center gap-1.5 rounded-md bg-sky-500 px-3 py-1.5 text-xs font-medium text-white transition-colors hover:bg-sky-600 disabled:opacity-50"
          >
            <Play size={13} />
            {busy ? "Running…" : "Run"}
          </button>
        </div>

        <RunLog entries={log} available={!!run?.available} />
      </div>
    </div>
  );
}

/**
 * The evaluation log, newest first: each run's time, the expression it ran, and what
 * came back — a value, a CEL error, or an input error that never left the browser.
 */
function RunLog({
  entries,
  available,
}: {
  entries: CelRun[];
  available: boolean;
}) {
  return (
    <div className="octo-code min-h-0 flex-1 overflow-y-auto rounded-md border border-black/10 bg-zinc-50 p-2 text-xs dark:border-white/15 dark:bg-zinc-950">
      {entries.length === 0 ? (
        <p className="text-zinc-400 dark:text-zinc-500">
          {available
            ? "Nothing evaluated yet. ⌘/Ctrl + Enter runs the expression."
            : "Runner unavailable — start the dev runner to evaluate."}
        </p>
      ) : (
        <ul className="flex flex-col gap-1">
          {entries.map((e) => (
            <li key={e.id} className="flex gap-2">
              <span className="shrink-0 tabular-nums text-zinc-400">
                {e.at}
              </span>
              <span className="min-w-0 flex-1 whitespace-pre-wrap break-words">
                <span className="font-semibold text-zinc-600 dark:text-zinc-300">
                  {e.expression}
                </span>{" "}
                {e.error || (e.result && !e.result.ok) ? (
                  <span className="text-red-500">
                    {e.error ?? e.result?.error ?? "evaluation failed"}
                  </span>
                ) : (
                  <span className="text-emerald-600 dark:text-emerald-400">
                    {JSON.stringify(e.result?.result)}
                  </span>
                )}
              </span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
