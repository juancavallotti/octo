"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronDown, ChevronUp, Trash2 } from "lucide-react";
import { useRun } from "@/app/run/RunContext";

const MIN_HEIGHT = 120;
const MAX_HEIGHT = 480;
const DEFAULT_HEIGHT = 200;

/**
 * Docked bottom panel that streams the runner's logs. It only renders when a
 * runner is available. The height is adjustable by dragging the top divider, and
 * the panel can be collapsed to just its header. New lines auto-scroll to the
 * bottom while the user is already near it.
 */
export default function LogPanel() {
  const { available, running, logs, version, clearLogs } = useRun();
  const [height, setHeight] = useState(DEFAULT_HEIGHT);
  // Collapsed by default and follows the run state (opens when running), until
  // the user overrides it with the toggle. Derived rather than synced in an
  // effect so a run starting auto-expands the panel without a state write.
  const [override, setOverride] = useState<boolean | null>(null);
  const collapsed = override ?? !running;
  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true);

  useEffect(() => {
    if (collapsed) return;
    const el = scrollRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [logs, collapsed]);

  if (!available) return null;

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
        <span className="text-xs font-medium tracking-tight">
          Runner logs{running ? " — running" : ""}
        </span>
        {version && (
          <span className="text-xs text-zinc-400 tabular-nums dark:text-zinc-500">
            — {version}
          </span>
        )}
        <div className="ml-auto flex items-center gap-1">
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
      {!collapsed && (
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
      )}
    </section>
  );
}
