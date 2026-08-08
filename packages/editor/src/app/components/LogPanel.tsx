"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, ChevronUp, Copy, Trash2 } from "lucide-react";
import { useRun, type RunLogLine } from "../run/RunContext";
import { useFlowRun } from "../run/FlowRunContext";
import { useConsole, type ConsoleTab } from "../run/console";
import { useSuiteRun } from "../run/SuiteRunContext";
import DevEnvPanel from "./DevEnvPanel";
import ConsoleTabs from "./console/ConsoleTabs";
import LogsTab from "./console/LogsTab";
import ProblemsTab from "./console/ProblemsTab";
import ResultsTab from "./console/ResultsTab";
import TestsTab from "./console/TestsTab";

const MIN_HEIGHT = 120;
const MAX_HEIGHT = 480;
const DEFAULT_HEIGHT = 200;
// The console height is a workspace-wide preference (not per-integration), so a
// returning user keeps the layout they dragged to. All tabs share one height.
const HEIGHT_KEY = "octo.console.height";

/** What the clear button says, per tab. Absent means the tab has nothing to clear. */
const CLEAR_LABEL: Partial<Record<ConsoleTab, string>> = {
  logs: "Clear logs",
  results: "Clear output",
  tests: "Clear test results",
};

/** Read the persisted console height, clamped to bounds; DEFAULT_HEIGHT if none. */
function readStoredHeight(): number {
  if (typeof window === "undefined") return DEFAULT_HEIGHT;
  // localStorage can be absent (a non-browser test env) or throw on access (a sandboxed
  // frame, storage disabled by the user): a remembered height is a nicety, never worth
  // crashing the panel — and the whole editor with it — over.
  let raw: string | null = null;
  try {
    raw = window.localStorage?.getItem(HEIGHT_KEY) ?? null;
  } catch {
    return DEFAULT_HEIGHT;
  }
  const n = raw ? Number(raw) : NaN;
  if (!Number.isFinite(n)) return DEFAULT_HEIGHT;
  return Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, n));
}

const NO_LOGS: RunLogLine[] = [];

/**
 * The docked bottom console: Problems, Logs, Results, and the Dev .env editor. It only
 * renders when a runner is available. Height is adjustable by dragging the top divider,
 * and the panel collapses to its header.
 *
 * The tab and collapse state live in ConsoleProvider rather than here, because a flow
 * run needs to be able to open the panel on the tab that answers what the user just
 * asked — see FlowRunContext.
 */
export default function LogPanel() {
  const run = useRun();
  const flowRun = useFlowRun();
  const { tab, setTab, override, setOverride, openTo } = useConsole();
  const [height, setHeight] = useState(readStoredHeight);
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(
    () => () => {
      if (copiedTimer.current) clearTimeout(copiedTimer.current);
    },
    [],
  );

  const running = run?.running ?? false;
  const logs = run?.logs ?? NO_LOGS;
  const results = flowRun?.results ?? [];
  const suiteRun = useSuiteRun();
  // The badge counts what went WRONG, so a green run is quiet and a bad one is not.
  const testFailures = suiteRun?.outcome
    ? suiteRun.outcome.totals.failed + suiteRun.outcome.totals.errored
    : 0;
  const issues = run?.validation.issues ?? [];
  // Collapsed by default, following the run state, until the user overrides it.
  const collapsed = override ?? !running;

  // Pressing Run should surface the log stream. Snap on the false→true transition only,
  // so the user can switch away freely while a run continues.
  const prevRunning = useRef(running);
  useEffect(() => {
    if (running && !prevRunning.current) openTo("logs");
    prevRunning.current = running;
  }, [running, openTo]);

  if (!run || !run.available) return null;
  const { version, testUrl, clearLogs } = run;

  // The last run's failure, shown under the validation issues: a document can be
  // perfectly valid and still fail the moment it actually runs.
  const runErrors = results[0]?.error ? [results[0].error] : [];

  function startResize(e: React.PointerEvent) {
    e.preventDefault();
    const startY = e.clientY;
    const startHeight = height;
    let latest = startHeight;
    const onMove = (ev: PointerEvent) => {
      const next = startHeight + (startY - ev.clientY);
      latest = Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, next));
      setHeight(latest);
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      // Best-effort persist; see readStoredHeight on why localStorage may be unavailable.
      try {
        window.localStorage?.setItem(HEIGHT_KEY, String(latest));
      } catch {
        // Losing a remembered height is harmless.
      }
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }

  return (
    <section
      style={{ height: collapsed ? undefined : height }}
      className="relative shrink-0 border-t border-black/10 dark:border-white/10 flex flex-col bg-zinc-50 dark:bg-zinc-900"
    >
      {!collapsed && (
        <div
          role="separator"
          aria-orientation="horizontal"
          aria-label="Resize log panel"
          onPointerDown={startResize}
          className="absolute inset-x-0 top-0 h-1.5 -translate-y-1/2 cursor-row-resize hover:bg-sky-400/40"
        />
      )}

      {/* Header — clicking it toggles the panel; the buttons stop propagation. */}
      <div
        role="button"
        tabIndex={0}
        aria-label={collapsed ? "Expand log panel" : "Collapse log panel"}
        onClick={() => setOverride(!collapsed)}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") {
            e.preventDefault();
            setOverride(!collapsed);
          }
        }}
        className="flex cursor-pointer items-center gap-2 px-3 h-8 shrink-0 border-b border-black/5 dark:border-white/5 select-none"
      >
        <span
          aria-hidden
          className={`h-2 w-2 rounded-full ${running ? "bg-emerald-500" : "bg-zinc-400"}`}
        />
        <ConsoleTabs
          active={tab}
          running={running}
          counts={{
            problems: issues.length,
            results: results.length,
            tests: testFailures,
          }}
          onSelect={(next) => {
            setTab(next);
            if (collapsed) setOverride(false);
          }}
        />
        {/* The runner's version identifies the whole console, not just the log stream. */}
        {version && (
          <span className="shrink-0 text-xs text-zinc-400 tabular-nums dark:text-zinc-500">
            — {version}
          </span>
        )}
        {tab === "logs" && running && testUrl && (
          <>
            <a
              href={testUrl}
              target="_blank"
              rel="noreferrer"
              onClick={(e) => e.stopPropagation()}
              title="Open your running integration"
              className="truncate text-xs text-sky-600 hover:underline dark:text-sky-400"
            >
              🔗 {testUrl}
            </a>
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                navigator.clipboard.writeText(testUrl).then(() => {
                  setCopied(true);
                  if (copiedTimer.current) clearTimeout(copiedTimer.current);
                  copiedTimer.current = setTimeout(() => setCopied(false), 1500);
                });
              }}
              aria-label="Copy test URL"
              title={copied ? "Copied!" : "Copy test URL"}
              className="rounded p-1 text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
            >
              {copied ? (
                <Check className="h-3.5 w-3.5 text-emerald-500" />
              ) : (
                <Copy className="h-3.5 w-3.5" />
              )}
            </button>
          </>
        )}
        <div className="ml-auto flex items-center gap-1">
          {(tab === "logs" || tab === "results" || tab === "tests") && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                if (tab === "logs") clearLogs();
                else if (tab === "tests") suiteRun?.clear();
                else flowRun?.clear();
              }}
              aria-label={CLEAR_LABEL[tab] ?? "Clear"}
              title={CLEAR_LABEL[tab] ?? "Clear"}
              className="rounded p-1 text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          )}
          <button
            type="button"
            onClick={(e) => {
              e.stopPropagation();
              setOverride(!collapsed);
            }}
            aria-label={collapsed ? "Expand log panel" : "Collapse log panel"}
            title={collapsed ? "Expand" : "Collapse"}
            className="rounded p-1 text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
          >
            {collapsed ? (
              <ChevronUp className="h-3.5 w-3.5" />
            ) : (
              <ChevronDown className="h-3.5 w-3.5" />
            )}
          </button>
        </div>
      </div>

      {!collapsed && tab === "problems" && (
        <ProblemsTab issues={issues} runErrors={runErrors} />
      )}
      {!collapsed && tab === "logs" && <LogsTab logs={logs} running={running} />}
      {!collapsed && tab === "results" && <ResultsTab results={results} />}
      {!collapsed && tab === "tests" && <TestsTab />}
      {!collapsed && tab === "env" && <DevEnvPanel />}
    </section>
  );
}
