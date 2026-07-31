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
import { useEditorState } from "../state/editorState";

/**
 * The test-suite capability: the dolphin suites that test this document's flows, one
 * per flow, kept wherever the host keeps them.
 *
 * Shaped like the editor-meta capability next door, and for the same reason — the host
 * moves raw strings and decides where they live, while all the parsing stays as pure
 * code in this package. What it is NOT is the same kind of file:
 *
 *   `.octo/editor-meta.json`  design-time scratch. Undeclared, never shipped, and
 *                             losing it costs you the inputs and mocks you saved.
 *   `<flow>_test.yaml`        a real artifact. It is committed, CI runs it, and
 *                             `dolphin test` in a terminal gives the same verdict the
 *                             Testing tab does.
 *
 * That difference is the whole point of the tab, and it is why suites are not folded
 * into the meta file: a test you cannot commit is a test you cannot rely on.
 *
 * Suites are keyed by FLOW NAME, not by the flow's client id — which is minted fresh on
 * every parse and cannot survive a reload — and not by the document either, because
 * dolphin's format declares exactly one flow per file.
 */

/** How long to wait after an edit before writing, as the meta store's save does. */
const SAVE_DEBOUNCE_MS = 2000;

/** One stored suite: the flow it tests, and its YAML. */
export interface TestSuiteFile {
  /** The flow name this suite tests — the key everything else addresses it by. */
  flow: string;
  content: string;
}

/**
 * Where the suites live, decoupled from the host that stores them. The standalone app
 * writes them to disk beside the flows (so a terminal can run them); the platform keeps
 * them with the integration.
 */
export interface TestSuiteStore {
  /** Every suite stored against this document. */
  list(integrationId: string | null): Promise<TestSuiteFile[]>;
  /** Write one suite's YAML, creating it when it is new. */
  save(integrationId: string | null, flow: string, content: string): Promise<void>;
  /** Delete a suite. A flow with no suite is the normal starting state. */
  remove(integrationId: string | null, flow: string): Promise<void>;
  /**
   * Whether suites can be persisted for this document. An unsaved draft has nothing to
   * key by, so its suites live for the session and no longer.
   */
  canEdit(integrationId: string | null): boolean;
}

/** The capability as the UI uses it. */
interface TestSuiteValue {
  /** The suite testing this flow, or undefined when it has none yet. */
  suiteFor(flow: string): string | undefined;
  /** Every suite, in flow-name order. */
  all(): TestSuiteFile[];
  /** True once the first load has settled, so the UI can tell empty from not-yet. */
  loaded: boolean;
  /** Whether edits will outlive the session. */
  canPersist: boolean;
  /** Write a suite's YAML. Debounced; the last write within the window wins. */
  setSuite(flow: string, content: string): void;
  /** Delete a suite, immediately — a deletion is not something to leave pending. */
  removeSuite(flow: string): Promise<void>;
}

const TestSuiteContext = createContext<TestSuiteValue | null>(null);

export function TestSuiteProvider({
  store,
  reloadToken,
  children,
}: {
  store: TestSuiteStore | null;
  /**
   * Bumped by the host when something else wrote a suite for this document — an MCP
   * agent, today. Without it the tab shows what it read on mount until the user
   * navigates away and back, so an agent that wrote a test looks like it did nothing.
   */
  reloadToken?: string | number;
  children: ReactNode;
}) {
  const { state } = useEditorState();
  const documentKey = state.integration.id;

  const [suites, setSuites] = useState<Record<string, string>>({});
  const [loaded, setLoaded] = useState(false);
  /**
   * The flows edited since the last write. Only these are saved, so the debounce never
   * rewrites a suite the user did not touch — which would churn mtimes on files that a
   * terminal may be watching. A refresh reads it too, to know what not to overwrite.
   */
  const dirtyRef = useRef<Set<string>>(new Set());
  // Orders in-flight lists, so a slow one cannot land after a newer one (or after the
  // document changed) and put back what it read.
  const listSeq = useRef(0);

  const canPersist = !!store && !!documentKey && store.canEdit(documentKey);

  /**
   * Re-read every suite, keeping any the user is midway through editing.
   *
   * Keeping them is the whole difference between a refresh and a clobber. The token is
   * news from elsewhere, not permission to discard what someone is typing — and a
   * pending edit is about to be written anyway, so taking the stored copy would undo it
   * and then save the undo.
   */
  const refresh = useCallback(async () => {
    const seq = ++listSeq.current;
    if (!store || !documentKey) {
      setSuites({});
      return;
    }
    let files: TestSuiteFile[] = [];
    try {
      files = await store.list(documentKey);
    } catch {
      // Suites we cannot read are suites this document does not have, as far as the
      // tab is concerned. An unreadable store is not worth blocking the editor over.
    }
    if (seq !== listSeq.current) return;
    setSuites((current) => {
      const next: Record<string, string> = Object.fromEntries(
        files.map((f) => [f.flow, f.content]),
      );
      for (const flow of dirtyRef.current) {
        if (current[flow] !== undefined) next[flow] = current[flow];
      }
      return next;
    });
  }, [store, documentKey]);

  // Load whenever the open document changes. Nothing carries over: a different document
  // has different suites, and its pending edits belong to the document that left.
  useEffect(() => {
    let cancelled = false;
    dirtyRef.current = new Set();
    // Nothing to wait for without a store, and a moment of `loaded: false` would flash
    // an empty state on the way to the same empty state.
    if (!store || !documentKey) {
      setSuites({});
      setLoaded(true);
      return;
    }
    setLoaded(false);
    void refresh().then(() => {
      if (!cancelled) setLoaded(true);
    });
    return () => {
      cancelled = true;
    };
  }, [refresh, store, documentKey]);

  // Re-list when the host says something else wrote a suite. `loaded` is deliberately
  // left alone: the tab has content on screen, and blanking it to an empty state on the
  // way to the same content plus one would be a worse answer than the stale one.
  const seenToken = useRef(reloadToken);
  useEffect(() => {
    if (reloadToken === seenToken.current) return;
    seenToken.current = reloadToken;
    void refresh();
  }, [reloadToken, refresh]);

  // Persist, debounced, and only what changed here.
  useEffect(() => {
    if (dirtyRef.current.size === 0 || !canPersist || !store || !documentKey) return;
    const timer = setTimeout(() => {
      const pending = [...dirtyRef.current];
      dirtyRef.current = new Set();
      for (const flow of pending) {
        const content = suites[flow];
        if (content === undefined) continue; // removed before the write landed
        store.save(documentKey, flow, content).catch(() => {
          // A failed write is not worth interrupting an edit over; the next one retries.
        });
      }
    }, SAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [suites, canPersist, store, documentKey]);

  const value = useMemo<TestSuiteValue>(
    () => ({
      suiteFor: (flow) => suites[flow],
      all: () =>
        Object.entries(suites)
          .map(([flow, content]) => ({ flow, content }))
          .sort((a, b) => a.flow.localeCompare(b.flow)),
      loaded,
      canPersist,
      setSuite(flow, content) {
        dirtyRef.current.add(flow);
        setSuites((current) => ({ ...current, [flow]: content }));
      },
      async removeSuite(flow) {
        dirtyRef.current.delete(flow);
        setSuites((current) => {
          const next = { ...current };
          delete next[flow];
          return next;
        });
        if (canPersist && store && documentKey) await store.remove(documentKey, flow);
      },
    }),
    [suites, loaded, canPersist, store, documentKey],
  );

  return <TestSuiteContext.Provider value={value}>{children}</TestSuiteContext.Provider>;
}

/**
 * The test-suite capability, or null when no store is mounted — which is what the
 * Testing tab reads to decide whether to exist at all.
 */
export function useTestSuites(): TestSuiteValue | null {
  return useContext(TestSuiteContext);
}
