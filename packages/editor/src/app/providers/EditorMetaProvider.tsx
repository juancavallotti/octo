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
import { newId } from "../model/document";
import { useEditorState } from "../state/editorState";
import { fileMetaFor, parseEditorMeta, serializeEditorMeta, withFileMeta } from "../meta/parse";
import { flowIdNames, syncFlowNames } from "../meta/rename";
import { emptyMeta, type EditorMeta, type TestInput } from "../meta/types";

/**
 * The editor-meta capability: the saved test inputs a flow can be run with, kept in
 * `.octo/editor-meta.json` beside the flows. Like the dev-env store, the capability
 * only moves a raw string — the host decides where that string lives (the standalone
 * app's flows directory, the platform's orchestrator) and all the parsing is pure code
 * here. When no store is provided, or the document has never been saved, inputs still
 * work; they just live for the session rather than being written down.
 */

/** The resource this is stored as. Path-like, and hidden from the Resources view. */
export const EDITOR_META_RESOURCE = ".octo/editor-meta.json";

/** How long to wait after an edit before writing the file (as RunContext's sync does). */
const SAVE_DEBOUNCE_MS = 2000;

/** Where the meta file lives, decoupled from the host that stores it. */
export interface EditorMetaStore {
  /** Load the raw file content; "" when there is none. */
  load(integrationId: string | null): Promise<string>;
  /** Persist the raw file content. */
  save(integrationId: string | null, content: string): Promise<void>;
  /**
   * Whether meta can be persisted for this document. The platform cannot store a
   * resource for an unsaved draft (it has no id yet); standalone shares one file for
   * the whole flows directory, but still needs the document to have a name to key by.
   */
  canEdit(integrationId: string | null): boolean;
}

interface EditorMetaValue {
  /** The saved inputs for a flow, by its client id. */
  inputs(flowId: string): TestInput[];
  /** Whether edits will outlive the session. False for an unsaved draft. */
  canPersist: boolean;
  addInput(flowId: string, input: Omit<TestInput, "id">): TestInput;
  updateInput(flowId: string, input: TestInput): void;
  removeInput(flowId: string, inputId: string): void;
}

const EditorMetaContext = createContext<EditorMetaValue | null>(null);

export function EditorMetaProvider({
  store,
  children,
}: {
  store: EditorMetaStore | null;
  children: ReactNode;
}) {
  const { state } = useEditorState();
  const doc = state.document;
  const documentKey = state.integration.id;

  const [meta, setMeta] = useState<EditorMeta>(emptyMeta);
  // The names the flows had when we last looked, so a rename can be detected by the id
  // it happened to. Seeded on load, updated by the sync effect.
  const namesRef = useRef<Map<string, string>>(new Map());
  const dirtyRef = useRef(false);

  const canPersist = !!store && !!documentKey && store.canEdit(documentKey);

  // Load the file whenever the open document changes.
  useEffect(() => {
    let cancelled = false;
    namesRef.current = flowIdNames(doc);
    if (!store || !documentKey) {
      setMeta(emptyMeta());
      return;
    }
    store
      .load(documentKey)
      .then((content) => {
        if (!cancelled) setMeta(parseEditorMeta(content));
      })
      .catch(() => {
        // A meta file we cannot read is not worth an error: the editor simply has no
        // saved inputs for this document.
        if (!cancelled) setMeta(emptyMeta());
      });
    return () => {
      cancelled = true;
    };
    // `doc` is deliberately not a dependency: this reloads per document, not per edit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [store, documentKey]);

  // Follow flow renames. Client ids are stable within a session, so a changed name
  // under the same id is a rename; on a reload every id is new and this is a no-op.
  useEffect(() => {
    if (!documentKey) return;
    const next = flowIdNames(doc);
    const prev = namesRef.current;
    namesRef.current = next;

    setMeta((current) => {
      const file = fileMetaFor(current, documentKey);
      const { meta: moved, changed } = syncFlowNames(file, prev, next);
      if (!changed) return current;
      dirtyRef.current = true;
      return withFileMeta(current, documentKey, moved);
    });
  }, [doc, documentKey]);

  // Persist, debounced, and only what we changed ourselves.
  useEffect(() => {
    if (!dirtyRef.current || !canPersist || !store || !documentKey) return;
    const content = serializeEditorMeta(meta);
    const timer = setTimeout(() => {
      dirtyRef.current = false;
      store.save(documentKey, content).catch(() => {
        // Losing a test input is not worth interrupting the user's work over.
      });
    }, SAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [meta, canPersist, store, documentKey]);

  /** Apply `fn` to the open document's flow entry and mark the file dirty. */
  const edit = useCallback(
    (flowId: string, fn: (inputs: TestInput[]) => TestInput[]) => {
      const flow = doc.flows.find((f) => f.id === flowId);
      if (!flow) return;
      const key = documentKey ?? "";
      setMeta((current) => {
        const file = fileMetaFor(current, key);
        const entry = file.flows[flow.name] ?? { inputs: [] };
        const flows = {
          ...file.flows,
          [flow.name]: { ...entry, inputs: fn(entry.inputs) },
        };
        dirtyRef.current = true;
        return withFileMeta(current, key, { flows });
      });
    },
    [doc, documentKey],
  );

  const value = useMemo<EditorMetaValue>(
    () => ({
      inputs(flowId) {
        const flow = doc.flows.find((f) => f.id === flowId);
        if (!flow) return [];
        return fileMetaFor(meta, documentKey ?? "").flows[flow.name]?.inputs ?? [];
      },
      canPersist,
      addInput(flowId, input) {
        const created: TestInput = { ...input, id: newId() };
        edit(flowId, (inputs) => [...inputs, created]);
        return created;
      },
      updateInput(flowId, input) {
        edit(flowId, (inputs) => inputs.map((i) => (i.id === input.id ? input : i)));
      },
      removeInput(flowId, inputId) {
        edit(flowId, (inputs) => inputs.filter((i) => i.id !== inputId));
      },
    }),
    [doc, meta, documentKey, canPersist, edit],
  );

  return (
    <EditorMetaContext.Provider value={value}>{children}</EditorMetaContext.Provider>
  );
}

/** The editor-meta capability. Null only when no provider is mounted. */
export function useEditorMeta(): EditorMetaValue | null {
  return useContext(EditorMetaContext);
}
