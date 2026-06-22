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
import { loadDevEnv } from "@/app/state/devEnv";
import { bffRunTransport, type RunTransport } from "./transport";

/**
 * Owns the editor's RUN feature client-side: it tracks whether a runner is
 * available, starts/stops it via the injected transport, streams its logs, and —
 * while running — debounces document edits into config re-writes so the runner
 * hot-reloads. A single provider holds this so the RUN button and the log panel
 * share one connection and one source of truth. The transport (how the runner is
 * reached) is pluggable; everything else here is backend-agnostic client policy.
 */

const SYNC_DEBOUNCE_MS = 2000;
const MAX_CLIENT_LOGS = 5000;

export interface RunLogLine {
  seq: number;
  text: string;
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
  /** Absolute URL that proxies to the running networked integration, or null. */
  testUrl: string | null;
  start: () => Promise<void>;
  stop: () => Promise<void>;
  clearLogs: () => void;
}

const RunContext = createContext<RunContextValue | null>(null);

export function RunProvider({
  transport = bffRunTransport,
  children,
}: {
  transport?: RunTransport;
  children: ReactNode;
}) {
  const { state } = useEditorState();
  const doc = state.document;
  const integrationId = state.integration.id;

  const [available, setAvailable] = useState(false);
  const [running, setRunning] = useState(false);
  const [version, setVersion] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [logs, setLogs] = useState<RunLogLine[]>([]);
  const [testPath, setTestPath] = useState<string | null>(null);

  const unsubscribeRef = useRef<(() => void) | null>(null);
  const lastSeqRef = useRef<number>(-1);
  const lastYamlRef = useRef<string | null>(null);

  const validation = useMemo(() => validateDocument(doc), [doc]);

  const closeStream = useCallback(() => {
    unsubscribeRef.current?.();
    unsubscribeRef.current = null;
  }, []);

  const openStream = useCallback(() => {
    if (unsubscribeRef.current) return;
    unsubscribeRef.current = transport.subscribeLogs((seq, text) => {
      // The server replays its whole buffer on connect (and on auto-reconnect),
      // so drop anything we've already shown.
      if (Number.isFinite(seq) && seq <= lastSeqRef.current) return;
      if (Number.isFinite(seq)) lastSeqRef.current = seq;
      setLogs((prev) => {
        const next = [...prev, { seq, text }];
        return next.length > MAX_CLIENT_LOGS
          ? next.slice(next.length - MAX_CLIENT_LOGS)
          : next;
      });
    });
  }, [transport]);

  // On mount, learn whether RUN is available and reattach if a runner is already
  // live (e.g. after a page reload).
  useEffect(() => {
    let cancelled = false;
    transport
      .status()
      .then((s) => {
        if (cancelled) return;
        setAvailable(s.available);
        setVersion(s.version);
        setTestPath(s.testPath);
        if (s.running) {
          setRunning(true);
          openStream();
        }
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [transport, openStream]);

  // Tear the stream down if the provider unmounts.
  useEffect(() => closeStream, [closeStream]);

  const start = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const yaml = toRunnableYaml(doc);
      // Dev .env values for the declared variables, injected into the runner's
      // environment for this run only (never serialized into the YAML). Scoped by
      // the open integration id; blanks are dropped so the runtime default applies.
      const stored = loadDevEnv(integrationId);
      const devEnv: Record<string, string> = {};
      for (const v of doc.env) {
        const val = stored[v.name];
        if (val) devEnv[v.name] = val;
      }
      const snapshot = await transport.start({ yaml, devEnv });
      lastYamlRef.current = yaml;
      setLogs([]); // the server starts a fresh buffer for this run
      setRunning(true);
      setTestPath(snapshot.testPath ?? null);
      openStream();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }, [doc, integrationId, openStream, transport]);

  const stop = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      await transport.stop();
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setRunning(false);
      setTestPath(null);
      closeStream();
      setBusy(false);
    }
  }, [closeStream, transport]);

  // Resolve the BFF-relative test path to an absolute URL for display/linking. It
  // works under both local dev and the in-cluster /editor mount because it is
  // computed from the current origin.
  const testUrl = useMemo(
    () =>
      testPath && typeof window !== "undefined"
        ? new URL(testPath, window.location.origin).href
        : null,
    [testPath],
  );

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
      transport.sync({ yaml }).catch(() => {});
    }, SYNC_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [doc, running, validation.ok, transport]);

  const value: RunContextValue = {
    available,
    running,
    busy,
    error,
    logs,
    validation,
    version,
    testUrl,
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
