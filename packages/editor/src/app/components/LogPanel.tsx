"use client";

import { useEffect, useRef, useState } from "react";
import { Check, ChevronDown, ChevronUp, Copy, Trash2 } from "lucide-react";
import { useRun, type RunLogLine } from "../run/RunContext";
import DevEnvPanel from "./DevEnvPanel";

const MIN_HEIGHT = 120;
const MAX_HEIGHT = 480;
const DEFAULT_HEIGHT = 200;
// Stable reference for the no-capability case so the scroll effect's deps don't
// change every render.
const NO_LOGS: RunLogLine[] = [];

type ConsoleTab = "logs" | "env";

/**
 * Docked bottom panel with a tabbed console: the runner's live log stream and a
 * "Dev .env" editor for local secret values. It only renders when a runner is
 * available. The height is adjustable by dragging the top divider, and the panel
 * can be collapsed to just its header. Log lines auto-scroll to the bottom while
 * the user is already near it.
 */
export default function LogPanel() {
  const run = useRun();
  const [tab, setTab] = useState<ConsoleTab>("logs");
  const [height, setHeight] = useState(DEFAULT_HEIGHT);
  // Collapsed by default and follows the run state (opens when running), until
  // the user overrides it with the toggle. Derived rather than synced in an
  // effect so a run starting auto-expands the panel without a state write.
  const [override, setOverride] = useState<boolean | null>(null);
  // Brief "copied" confirmation on the test-URL copy button.
  const [copied, setCopied] = useState(false);
  const copiedTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => {
    if (copiedTimer.current) clearTimeout(copiedTimer.current);
  }, []);
  // Safe reads so all hooks run unconditionally even with no run capability.
  const running = run?.running ?? false;
  const logs = run?.logs ?? NO_LOGS;
  const collapsed = override ?? !running;
  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true);

  useEffect(() => {
    if (collapsed || tab !== "logs") return;
    const el = scrollRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [logs, collapsed, tab]);

  // No RunProvider mounted, or no runner available => no log panel.
  if (!run || !run.available) return null;
  const { version, testUrl, clearLogs } = run;

  function startResize(e: React.PointerEvent) {
    e.preventDefault();
    const startY = e.clientY;
    const startHeight = height;
    const onMove = (ev: PointerEvent) => {
      // Dragging the top edge upwards grows the panel.
      const next = startHeight + (startY - ev.clientY);
      setHeight(Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, next)));
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
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

      {/* Header — clicking anywhere on it toggles the panel; the action buttons
          stop propagation so they keep their own behavior. */}
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
        <div className="flex items-center gap-0.5">
          {(
            [
              ["logs", `Logs${running ? " — running" : ""}`],
              ["env", "Dev .env"],
            ] as const
          ).map(([key, label]) => (
            <button
              key={key}
              type="button"
              aria-pressed={tab === key}
              onClick={(e) => {
                e.stopPropagation();
                setTab(key);
                if (collapsed) setOverride(false);
              }}
              className={`rounded px-2 py-0.5 text-xs font-medium tracking-tight transition-colors ${
                tab === key
                  ? "bg-black/[0.06] text-zinc-800 dark:bg-white/10 dark:text-zinc-100"
                  : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
              }`}
            >
              {label}
            </button>
          ))}
        </div>
        {tab === "logs" && version && (
          <span className="text-xs text-zinc-400 tabular-nums dark:text-zinc-500">
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
          {tab === "logs" && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                clearLogs();
              }}
              aria-label="Clear logs"
              title="Clear logs"
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

      {/* Body */}
      {!collapsed &&
        (tab === "logs" ? (
        <div
          ref={scrollRef}
          onScroll={onScroll}
          className="flex-1 overflow-auto px-3 py-2 font-mono text-xs leading-relaxed text-zinc-700 dark:text-zinc-300"
        >
          {logs.length === 0 ? (
            <p className="text-zinc-400 dark:text-zinc-600">
              No output yet. Press Run to start the integration.
            </p>
          ) : (
            logs.map((line) => (
              <div key={line.seq} className="whitespace-pre-wrap break-words">
                {line.text || " "}
              </div>
            ))
          )}
        </div>
        ) : (
          <DevEnvPanel />
        ))}
    </section>
  );
}
