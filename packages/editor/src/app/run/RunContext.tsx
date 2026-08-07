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
import { toRunnableYaml } from "../model/runConfig";
import { validateDocument, type ValidationResult } from "../model/validate";
import { useEditorState } from "../state/editorState";
import {
  type RunTarget,
  type RunTransport,
  type CelEvalRequest,
  type CelEvalResult,
  type TestRunOutcome,
  type TestRunRequest,
} from "./transport";

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
// How often to re-read status while a networked run's URL has not landed yet. The endpoint
// is withheld until the run's pod is ready, and status is otherwise read only on mount, so
// this is what makes the link appear on its own rather than on a page reload.
const URL_POLL_MS = 2000;

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
  /**
   * Whether the dolphin binary is configured. Separate from {@link available} because
   * the two are separate binaries: a host can ship octo and not dolphin, in which case
   * flows still run and only the Testing tab's Run goes dead.
   */
  testAvailable: boolean;
  /** dolphin's `version` line, or null when unknown/unavailable. */
  testVersion: string | null;
  /** Absolute URL of the running networked integration, or null. */
  testUrl: string | null;
  /**
   * Whether the running app follows SAVES rather than the editor's buffer. Surfaced so
   * the RUN panel can say which it is: on a host that reloads on save, an unsaved edit
   * deliberately does not reach the running app, and a panel that implied otherwise
   * would be the most confusing thing in the feature.
   */
  reloadsOnSave: boolean;
  start: () => Promise<void>;
  stop: () => Promise<void>;
  clearLogs: () => void;
  /** Evaluate a one-shot CEL expression (CEL tester); delegates to the transport. */
  evalCel: (req: CelEvalRequest) => Promise<CelEvalResult>;
  /**
   * Run dolphin suites; delegates to the transport, like evalCel. Stateless here on
   * purpose: a test run starts no long-lived process, so there is nothing for this
   * provider to track. Whether a run is in flight, and what it produced, belong to the
   * tab that asked.
   */
  runTests: (req: TestRunRequest) => Promise<TestRunOutcome>;
}

const RunContext = createContext<RunContextValue | null>(null);

export function RunProvider({
  transport,
  children,
}: {
  transport: RunTransport;
  children: ReactNode;
}) {
  const { state } = useEditorState();
  const doc = state.document;
  const integrationId = state.integration.id;

  const [available, setAvailable] = useState(false);
  const [running, setRunning] = useState(false);
  const [version, setVersion] = useState<string | null>(null);
  const [testAvailable, setTestAvailable] = useState(false);
  const [testVersion, setTestVersion] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [logs, setLogs] = useState<RunLogLine[]>([]);
  // What the host reported as the run's address: app-relative from a host that proxies to
  // its own child process, absolute from one that gave the run its own hostname. Resolved
  // to an absolute URL below, which is the only form a consumer sees.
  const [testAddress, setTestAddress] = useState<string | null>(null);
  // Whether this run will ever publish a URL. Held separately from the address because the
  // two come apart while a dev-run pod comes up — exposable is known at once, the URL only
  // when it is ready — and it is that gap the URL poll below exists to close.
  const [exposable, setExposable] = useState(false);
  const [reloadsOnSave, setReloadsOnSave] = useState(false);

  const unsubscribeRef = useRef<(() => void) | null>(null);
  const lastSeqRef = useRef<number>(-1);
  const lastYamlRef = useRef<string | null>(null);

  const validation = useMemo(() => validateDocument(doc), [doc]);

  // Which run every call addresses. A draft has no id, and a host that runs the app
  // elsewhere cannot address one at all — which is why start refuses it there.
  const target = useMemo<RunTarget>(
    () => ({ integrationId: integrationId ?? undefined }),
    [integrationId],
  );

  const closeStream = useCallback(() => {
    unsubscribeRef.current?.();
    unsubscribeRef.current = null;
  }, []);

  // The target is an argument rather than a dependency, so opening a stream does not
  // depend on the identity of the current one: a stream belongs to the run it was opened
  // for, and re-creating this callback whenever the open integration changed would only
  // make the guard below decide that a stream was already open.
  const openStream = useCallback(
    (run: RunTarget) => {
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
      }, run);
    },
    [transport],
  );

  // On mount — and whenever the open integration changes — learn whether RUN is available
  // for THIS target and reattach if its runner is already live (e.g. after a page reload,
  // or when switching back to an integration left running elsewhere).
  useEffect(() => {
    let cancelled = false;
    transport
      .status(target)
      .then((s) => {
        if (cancelled) return;
        setAvailable(s.available);
        setVersion(s.version);
        setTestAvailable(s.testAvailable);
        setTestVersion(s.testVersion);
        setTestAddress(s.testUrl);
        setExposable(s.exposable);
        setReloadsOnSave(s.reloadsOnSave);
        if (s.running) {
          setRunning(true);
          openStream(target);
        }
      })
      .catch(() => {});
    // On a target change the previous integration's run is no longer what this provider
    // tracks, so reset to a clean slate before the status check above re-decides for the
    // new one. Without this the old stream stays open (openStream's guard would refuse a
    // fresh one, so the panel keeps showing the previous integration's logs), `running`
    // sticks true from the old integration, and Stop would target the newly-opened one
    // while the old run kept going. Also runs on unmount, which is harmless.
    return () => {
      cancelled = true;
      closeStream();
      setRunning(false);
      setTestAddress(null);
      setExposable(false);
      lastSeqRef.current = -1;
      lastYamlRef.current = null;
    };
  }, [transport, openStream, closeStream, target]);

  const start = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      const yaml = toRunnableYaml(doc);
      // Dev-env values are no longer sent from the browser: run-host stages the
      // integration's `.env.dev` resource (edited in the Dev .env panel) and the
      // runtime loads it, so a run reads its credentials from the host's store.
      const snapshot = await transport.start({ ...target, yaml });
      lastYamlRef.current = yaml;
      setLogs([]); // the server starts a fresh buffer for this run
      setRunning(true);
      setTestAddress(snapshot.testUrl ?? null);
      setExposable(snapshot.exposable);
      setReloadsOnSave(snapshot.reloadsOnSave);
      openStream(target);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }, [doc, openStream, target, transport]);

  const stop = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      await transport.stop(target);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setRunning(false);
      setTestAddress(null);
      setExposable(false);
      closeStream();
      setBusy(false);
    }
  }, [closeStream, target, transport]);

  // Resolve the host's address into an absolute URL for display/linking. It works for
  // both kinds: a relative path resolves against the current origin (so it is right under
  // local dev and under the in-cluster /editor mount alike), and an absolute URL — a run
  // with a hostname of its own — ignores the base and passes straight through.
  const testUrl = useMemo(
    () =>
      testAddress && typeof window !== "undefined"
        ? new URL(testAddress, window.location.origin).href
        : null,
    [testAddress],
  );

  // Poll for a networked run's URL while it is still coming up. The address is learned only
  // from a status read, and a dev-run pod withholds it until its pod is ready (offering it
  // sooner would hand out a link that 502s while the image pulls). Status is otherwise read
  // once, on mount — so without this the log panel's endpoint link stays blank from the
  // moment Run is clicked until a full page reload.
  //
  // It runs only while a run that WILL have a URL (exposable) does not have it yet, and stops
  // the instant one lands — the address setting re-runs this effect, and the guard then bows
  // out. A run that serves no HTTP is not exposable and so never polls; the local runner
  // reports its URL at start, so it has nothing to wait for either. A run that never becomes
  // ready keeps polling until it is stopped or the editor moves on, which is the honest cost
  // of the link appearing on its own: a cheap status read every couple of seconds.
  useEffect(() => {
    if (!running || !exposable || testAddress) return;
    let cancelled = false;
    const id = setInterval(() => {
      transport
        .status(target)
        .then((s) => {
          if (cancelled) return;
          setExposable(s.exposable);
          setTestAddress(s.testUrl);
        })
        .catch(() => {});
    }, URL_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [running, exposable, testAddress, transport, target]);

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
  //
  // Skipped entirely on a host that reloads on save. There the running app reads the
  // STORED definition, so a pushed buffer is not merely redundant — nothing reads it, and
  // pushing one would leave the panel implying an edit had taken effect when it had not.
  // The save path is the trigger there, server-side, which is what makes it cover every
  // writer rather than only this editor.
  useEffect(() => {
    if (reloadsOnSave || !running || !validation.ok) return;
    const yaml = toRunnableYaml(doc);
    if (yaml === lastYamlRef.current) return;
    const t = setTimeout(() => {
      lastYamlRef.current = yaml;
      transport.sync({ ...target, yaml }).catch(() => {});
    }, SYNC_DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [doc, running, validation.ok, transport, target, reloadsOnSave]);

  const evalCel = useCallback(
    (req: CelEvalRequest): Promise<CelEvalResult> => transport.evalCel(req),
    [transport],
  );

  const runTests = useCallback(
    (req: TestRunRequest): Promise<TestRunOutcome> => transport.test(req),
    [transport],
  );

  const value: RunContextValue = {
    available,
    running,
    busy,
    error,
    logs,
    validation,
    version,
    testAvailable,
    testVersion,
    testUrl,
    reloadsOnSave,
    start,
    stop,
    clearLogs,
    evalCel,
    runTests,
  };

  return <RunContext.Provider value={value}>{children}</RunContext.Provider>;
}

/**
 * The current run capability, or null when no RunProvider is mounted (the RUN
 * capability is absent). Consumers render their run controls only when non-null.
 */
export function useRun(): RunContextValue | null {
  return useContext(RunContext);
}
