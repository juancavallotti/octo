"use client";

import { useEffect, useRef } from "react";
import { Plus, SquareTerminal, Trash2 } from "lucide-react";
import "@xterm/xterm/css/xterm.css";
import { useTerminal } from "../../providers/TerminalProvider";

/**
 * The Terminal tab: the active shell on the left, every session on the right, laid out
 * as VS Code's terminal panel is. Showing the tab with no session running starts one,
 * since a terminal tab with nothing in it has nothing to offer but a button.
 */
export default function TerminalTab() {
  const terminal = useTerminal();
  const viewport = useRef<HTMLDivElement>(null);

  // On arrival only: a shell the user just closed is not one they want back.
  const ensure = terminal?.ensure;
  useEffect(() => {
    void ensure?.();
  }, [ensure]);

  const attach = terminal?.attach;
  useEffect(() => {
    if (!attach || !viewport.current) return;
    return attach(viewport.current);
  }, [attach]);

  // The panel is resized by dragging its divider, which nothing tells the terminal about.
  const fit = terminal?.fit;
  useEffect(() => {
    const el = viewport.current;
    if (!el || !fit || typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(() => fit());
    observer.observe(el);
    return () => observer.disconnect();
  }, [fit]);

  if (!terminal) return null;
  const { sessions, active, error, select, close } = terminal;

  return (
    <div className="flex flex-1 min-h-0">
      <div className="relative flex-1 min-w-0 py-1 pl-3">
        <div ref={viewport} data-testid="terminal-viewport" className="h-full w-full" />
        {/* Over the viewport only while it is empty: a live shell's output is not
            something to cover. With one open, the session list says it instead. */}
        {error && sessions.length === 0 && (
          <p className="absolute inset-x-3 top-2 text-xs text-red-600 dark:text-red-400">
            Could not start a terminal: {error}
          </p>
        )}
        {!error && sessions.length === 0 && (
          <p className="absolute inset-x-3 top-2 text-xs text-zinc-400 dark:text-zinc-500">
            No terminal open. Press + to start one.
          </p>
        )}
      </div>
      <div className="flex w-44 shrink-0 flex-col border-l border-black/5 dark:border-white/5">
        <div className="flex items-center justify-end gap-1 px-1 py-0.5">
          {error && sessions.length > 0 && (
            <span
              role="alert"
              title={`Could not start a terminal: ${error}`}
              className="min-w-0 flex-1 truncate pl-1 text-xs text-red-600 dark:text-red-400"
            >
              Could not start a terminal
            </span>
          )}
          <button
            type="button"
            onClick={() => void terminal.open()}
            aria-label="New terminal"
            title="New terminal (Ctrl+Shift+`)"
            className="rounded p-1 text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
          >
            <Plus className="h-3.5 w-3.5" />
          </button>
        </div>
        <ul aria-label="Terminals" className="flex-1 overflow-auto">
          {sessions.map((s) => {
            const selected = s.id === active;
            return (
              <li key={s.id}>
                <div
                  className={`group flex items-center gap-1.5 px-2 py-0.5 text-xs ${
                    selected
                      ? "bg-black/[0.06] text-zinc-800 dark:bg-white/10 dark:text-zinc-100"
                      : "text-zinc-500 hover:text-zinc-700 dark:hover:text-zinc-300"
                  }`}
                >
                  <button
                    type="button"
                    aria-pressed={selected}
                    onClick={() => select(s.id)}
                    className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
                  >
                    <SquareTerminal className="h-3.5 w-3.5 shrink-0" aria-hidden />
                    <span className="truncate">{s.title}</span>
                    {s.exitCode !== null && (
                      <span className="shrink-0 text-zinc-400 dark:text-zinc-500">exited</span>
                    )}
                  </button>
                  <button
                    type="button"
                    onClick={() => close(s.id)}
                    aria-label={`Kill ${s.title}`}
                    title="Kill terminal"
                    className="rounded p-0.5 opacity-0 hover:bg-black/5 group-hover:opacity-100 focus:opacity-100 dark:hover:bg-white/10"
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      </div>
    </div>
  );
}
