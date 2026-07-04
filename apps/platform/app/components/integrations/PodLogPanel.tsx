"use client";

import { useEffect, useRef, useState } from "react";
import { ScrollText, Trash2, X } from "lucide-react";

/**
 * Dockable, resizable panel that live-tails one pod's logs, styled after the
 * editor's LogPanel. It streams the BFF proxy (which relays the orchestrator's
 * k8s log stream) via fetch, decoding the chunked text into lines and
 * auto-scrolling to the bottom while the user is already near it. Selecting a
 * different pod remounts it (keyed by pod name upstream), so the stream and
 * buffer reset cleanly. Closing aborts the fetch, which tears down the stream all
 * the way to the cluster.
 */

const MIN_HEIGHT = 120;
const MAX_HEIGHT = 480;
const DEFAULT_HEIGHT = 220;
// Panel height is a workspace-wide preference, shared with the editor console.
const HEIGHT_KEY = "octo.podlogs.height";
// Cap the retained buffer so an old, chatty pod can't grow the DOM unbounded.
const MAX_LINES = 5000;

function readStoredHeight(): number {
  if (typeof window === "undefined") return DEFAULT_HEIGHT;
  const raw = window.localStorage.getItem(HEIGHT_KEY);
  const n = raw ? Number(raw) : NaN;
  if (!Number.isFinite(n)) return DEFAULT_HEIGHT;
  return Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, n));
}

type Status = "connecting" | "streaming" | "ended" | "error";

export default function PodLogPanel({
  deploymentId,
  podName,
  onClose,
}: {
  deploymentId: string;
  podName: string;
  onClose: () => void;
}) {
  const [lines, setLines] = useState<string[]>([]);
  const [status, setStatus] = useState<Status>("connecting");
  const [error, setError] = useState<string | null>(null);
  const [height, setHeight] = useState(readStoredHeight);
  const scrollRef = useRef<HTMLDivElement>(null);
  const pinnedRef = useRef(true);

  // Open the stream for this pod; abort it on unmount. The panel is keyed by pod
  // upstream, so a different pod remounts it — the effect runs once per instance
  // and the initial state ([]/no error/connecting) is already the reset state, so
  // there is no synchronous setState reset here.
  useEffect(() => {
    const ctrl = new AbortController();
    let cancelled = false;

    const append = (parts: string[]) => {
      if (!parts.length) return;
      setLines((prev) => {
        const next = prev.concat(parts);
        return next.length > MAX_LINES ? next.slice(next.length - MAX_LINES) : next;
      });
    };

    (async () => {
      try {
        const res = await fetch(
          `/api/deployments/${encodeURIComponent(deploymentId)}/pods/${encodeURIComponent(podName)}/logs?follow=1`,
          { signal: ctrl.signal, cache: "no-store" },
        );
        if (!res.ok || !res.body) {
          const body = (await res.json().catch(() => null)) as { error?: string } | null;
          if (!cancelled) {
            setStatus("error");
            setError(body?.error ?? `Logs unavailable (${res.status})`);
          }
          return;
        }
        if (!cancelled) setStatus("streaming");
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          const parts = buf.split("\n");
          buf = parts.pop() ?? "";
          append(parts);
        }
        if (buf) append([buf]);
        if (!cancelled) setStatus("ended");
      } catch (e) {
        if ((e as Error).name !== "AbortError" && !cancelled) {
          setStatus("error");
          setError((e as Error).message);
        }
      }
    })();

    return () => {
      cancelled = true;
      ctrl.abort();
    };
  }, [deploymentId, podName]);

  // Auto-scroll to the bottom on new lines while the user is pinned there.
  useEffect(() => {
    const el = scrollRef.current;
    if (el && pinnedRef.current) el.scrollTop = el.scrollHeight;
  }, [lines]);

  function onScroll() {
    const el = scrollRef.current;
    if (!el) return;
    pinnedRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24;
  }

  function startResize(e: React.PointerEvent) {
    e.preventDefault();
    const startY = e.clientY;
    const startHeight = height;
    let latest = startHeight;
    const onMove = (ev: PointerEvent) => {
      const next = startHeight + (startY - ev.clientY); // drag up grows the panel
      latest = Math.min(MAX_HEIGHT, Math.max(MIN_HEIGHT, next));
      setHeight(latest);
    };
    const onUp = () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.localStorage.setItem(HEIGHT_KEY, String(latest));
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
  }

  const live = status === "streaming";

  return (
    <section
      style={{ height }}
      className="relative flex shrink-0 flex-col border-t border-black/10 bg-zinc-50 dark:border-white/10 dark:bg-zinc-900"
    >
      <div
        role="separator"
        aria-orientation="horizontal"
        aria-label="Resize log panel"
        onPointerDown={startResize}
        className="absolute inset-x-0 top-0 h-1.5 -translate-y-1/2 cursor-row-resize hover:bg-sky-400/40"
      />

      <div className="flex h-8 shrink-0 select-none items-center gap-2 border-b border-black/5 px-3 dark:border-white/5">
        <ScrollText size={14} className="text-zinc-500" />
        <span
          aria-hidden
          className={`h-2 w-2 rounded-full ${live ? "bg-emerald-500" : status === "error" ? "bg-red-500" : "bg-zinc-400"}`}
        />
        <span className="truncate font-mono text-xs text-zinc-600 dark:text-zinc-300">
          {podName}
        </span>
        <span className="text-xs text-zinc-400">
          {status === "connecting"
            ? "connecting…"
            : live
              ? "tailing"
              : status === "ended"
                ? "stream ended"
                : "error"}
        </span>
        <div className="ml-auto flex items-center gap-1">
          <button
            type="button"
            onClick={() => setLines([])}
            aria-label="Clear logs"
            title="Clear"
            className="rounded p-1 text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
          >
            <Trash2 className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close log panel"
            title="Close"
            className="rounded p-1 text-zinc-500 hover:bg-black/5 dark:hover:bg-white/10"
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>
      </div>

      <div
        ref={scrollRef}
        onScroll={onScroll}
        className="flex-1 overflow-auto px-3 py-2 font-mono text-xs leading-relaxed text-zinc-700 dark:text-zinc-300"
      >
        {error ? (
          <p className="text-red-500">{error}</p>
        ) : lines.length === 0 ? (
          <p className="text-zinc-400 dark:text-zinc-600">
            {status === "ended" ? "No output." : "Waiting for output…"}
          </p>
        ) : (
          lines.map((line, i) => (
            <div key={i} className="whitespace-pre-wrap break-words">
              {line || " "}
            </div>
          ))
        )}
      </div>
    </section>
  );
}
