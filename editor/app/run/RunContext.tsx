"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { toRunnableYaml } from "@/app/model/runConfig";
import { validateDocument, type ValidationResult } from "@/app/model/validate";
import { useEditorState } from "@/app/state/editorState";

/**
 * Owns the editor's RUN feature client-side: it tracks whether a runner is
 * available, starts/stops it via the run API, streams its logs over SSE, and —
 * while running — debounces document edits into config re-writes so the runner
 * hot-reloads. A single provider holds this so the RUN button and the log panel
 * share one connection and one source of truth.
 */

const SYNC_DEBOUNCE_MS = 2000;
const MAX_CLIENT_LOGS = 5000;

export interface RunLogLine {
  seq: number;
  text: string;
}

interface RunStatusResponse {
  available: boolean;
  running: boolean;
  version: string | null;
}

interface RunContextValue {
  available: boolean;
  running: boolean;
  busy: boolean;
  error: string | null;
  logs: RunLogLine[];
  validation: ValidationResult;
  /** The runner's `--version` line, or null when unknown/unavailable. */
  version: string | null;
  start: () => Promise<void>;
  stop: () => Promise<void>;
  clearLogs: () => void;
}

const RunContext = createContext<RunContextValue | null>(null);

export function RunProvider({ children }: { children: ReactNode }) {
  const { state } = useEditorState();
  const doc = state.document;

  const [available, setAvailable] = useState(false);
  const [running, setRunning] = useState(false);
  const [version, setVersion] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [logs, setLogs] = useState<RunLogLine[]>([]);

  const sourceRef = useRef<EventSource | null>(null);
  const lastSeqRef = useRef<number>(-1);
  const lastYamlRef = useRef<string | null>(null);

  const validation = useMemo(() => validateDocument(doc), [doc]);

  const closeStream = useCallback(() => {
    sourceRef.current?.close();
    sourceRef.current = null;
  }, []);

  const openStream = useCallback(() => {
    if (sourceRef.current) return;
    const es = new EventSource("/api/run/logs");
    es.onmessage = (ev) => {
      const seq = Number(ev.lastEventId);
      // The server replays its whole buffer on connect (and on auto-reconnect),
      // so drop anything we've already shown.
      if (Number.isFinite(seq) && seq <= lastSeqRef.current) return;
      if (Number.isFinite(seq)) lastSeqRef.current = seq;
      setLogs((prev) => {
        const next = [...prev, { seq, text: ev.data }];
        return next.length > MAX_CLIENT_LOGS
          ? next.slice(next.length - MAX_CLIENT_LOGS)
          : next;
      });
    };
    sourceRef.current = es;
  }, []);

  // On mount, learn whether RUN is available and reattach if a runner is already
  // live (e.g. after a page reload).
  useEffect(() => {
    let cancelled = false;
    fetch("/api/run")
      .then((r) => r.json() as Promise<RunStatusResponse>)
      .then((s) => {
        if (cancelled) return;
        setAvailable(s.available);
        setVersion(s.version);
        if (s.running) {
          setRunning(true);
          openStream();
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [openStream]);

  // Tear the stream down if the provider unmounts.
  useEffect(() => closeStream, [closeStream]);

  const start = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const yaml = toRunnableYaml(doc);
      const res = await fetch("/api/run/start", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ yaml }),
      });
      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.error ?? `start failed (${res.status})`);
      }
      lastYamlRef.current = yaml;
      setLogs([]); // the server starts a fresh buffer for this run
      setRunning(true);
      openStream();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }, [doc, openStream]);

  const stop = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      await fetch("/api/run/stop", { method: "POST" });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setRunning(false);
      closeStream();
      setBusy(false);
    }
  }, [closeStream]);

  // Clear only the client-side display. We deliberately leave the open stream and
  // `lastSeqRef` untouched: reconnecting would make the server replay its whole
  // buffer, and resetting the seq cursor would let those replayed lines back in —
  // so the cleared logs would immediately reappear while running. Keeping the
  // cursor means any later replay (e.g. an auto-reconnect) stays deduped.
  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  // While running, push debounced edits to the watched config so octo reloads.
  // Only valid documents are synced: pushing an invalid intermediate edit (e.g.
  // mid-rename) would make the live runner fail its hot-reload. We hold the last
  // valid config until the document is valid again, then push the difference.
  useEffect(() => {
    if (!running || !validation.ok) return;
    const yaml = toRunnableYaml(doc);
    if (yaml === lastYamlRef.current) return;
    const t = setTimeout(() => {
      lastYamlRef.current = yaml;
      fetch("/api/run/sync", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ yaml }),
      }).catch(() => {});
    }, SYNC_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [doc, running, validation.ok]);

  const value: RunContextValue = {
    available,
    running,
    busy,
    error,
    logs,
    validation,
    version,
    start,
    stop,
    clearLogs,
  };

  return <RunContext.Provider value={value}>{children}</RunContext.Provider>;
}

export function useRun(): RunContextValue {
  const ctx = useContext(RunContext);
  if (!ctx) throw new Error("useRun must be used within a RunProvider");
  return ctx;
}
