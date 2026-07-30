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
  children,
}: {
  store: TestSuiteStore | null;
  children: ReactNode;
}) {
  const { state } = useEditorState();
  const documentKey = state.integration.id;

  const [suites, setSuites] = useState<Record<string, string>>({});
  const [loaded, setLoaded] = useState(false);
  /**
   * The flows edited since the last write. Only these are saved, so the debounce never
   * rewrites a suite the user did not touch — which would churn mtimes on files that a
   * terminal may be watching.
   */
  const dirtyRef = useRef<Set<string>>(new Set());

  const canPersist = !!store && !!documentKey && store.canEdit(documentKey);

  // Load whenever the open document changes.
  useEffect(() => {
    let cancelled = false;
    setLoaded(false);
    dirtyRef.current = new Set();
    if (!store || !documentKey) {
      setSuites({});
      setLoaded(true);
      return;
    }
    store
      .list(documentKey)
      .then((files) => {
        if (cancelled) return;
        setSuites(Object.fromEntries(files.map((f) => [f.flow, f.content])));
        setLoaded(true);
      })
      .catch(() => {
        // Suites we cannot read are suites this document does not have, as far as the
        // tab is concerned. An unreadable store is not worth blocking the editor over.
        if (cancelled) return;
        setSuites({});
        setLoaded(true);
      });
    return () => {
      cancelled = true;
    };
  }, [store, documentKey]);

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
