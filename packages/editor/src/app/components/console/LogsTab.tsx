"use client";

import { useEffect, useRef } from "react";
import type { RunLogLine } from "../../run/RunContext";

/**
 * The runner's live log stream. Auto-scrolls while the user is already at the bottom —
 * scroll up to read something and the stream stops yanking you back down.
 */
export default function LogsTab({ logs }: { logs: RunLogLine[] }) {
  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true);

  useEffect(() => {
    const el = scrollRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [logs]);

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
  }

  return (
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
            {line.text || " "}
          </div>
        ))
      )}
    </div>
  );
}
