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
import {
  emptyFlowMeta,
  emptyMeta,
  type BlockMock,
  type EditorMeta,
  type FlowMeta,
  type TestInput,
} from "../meta/types";

/**
 * The editor-meta capability: what a flow can be run with and run under — its saved test
 * inputs, the blocks mocked out, the blocks spied on — kept in `.octo/editor-meta.json`
 * beside the flows. Like the dev-env store, the capability only moves a raw string: the
 * host decides where that string lives (the standalone app's flows directory, the
 * platform's orchestrator) and all the parsing is pure code here. When no store is
 * provided, or the document has never been saved, all of it still works; it just lives
 * for the session rather than being written down.
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

/**
 * The capability, as the UI uses it. Inputs are addressed by an id of our own; mocks and
 * spies by the runtime's block address, because that is the only name the runner knows a
 * block by (see run/address.ts). Both are scoped to a flow, by its client id.
 */
interface EditorMetaValue {
  /** The saved inputs for a flow, by its client id. */
  inputs(flowId: string): TestInput[];
  /** Whether edits will outlive the session. False for an unsaved draft. */
  canPersist: boolean;
  addInput(flowId: string, input: Omit<TestInput, "id">): TestInput;
  updateInput(flowId: string, input: TestInput): void;
  removeInput(flowId: string, inputId: string): void;

  /** The mock on the block at `address`, if it has one (enabled or not). */
  mockAt(flowId: string, address: string): BlockMock | undefined;
  /** Save the mock for its address, replacing any mock already there. */
  setMock(flowId: string, mock: BlockMock): void;
  removeMock(flowId: string, address: string): void;

  /** The addresses spied in this flow. */
  spies(flowId: string): string[];
  /** Turn the spy on the block at `address` on or off. */
  setSpy(flowId: string, address: string, on: boolean): void;

  /** Every enabled mock in the document — what a run is given. */
  enabledMocks(): BlockMock[];
  /** Every spied address in the document — what a run is given. */
  allSpies(): string[];
}

const EditorMetaContext = createContext<EditorMetaValue | null>(null);

export function EditorMetaProvider({
  store,
  reloadToken,
  children,
}: {
  store: EditorMetaStore | null;
  /**
   * Bumped by the host when something else wrote this file — an MCP agent placing a
   * mock or saving a test input. Without it the canvas shows what it read on mount,
   * and an agent that mocked a block looks to the user like it did nothing.
   */
  reloadToken?: string | number;
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
  // Orders in-flight loads, so a slow one cannot land after a newer one.
  const loadSeq = useRef(0);

  const canPersist = !!store && !!documentKey && store.canEdit(documentKey);

  const refresh = useCallback(async () => {
    const seq = ++loadSeq.current;
    if (!store || !documentKey) {
      setMeta(emptyMeta());
      return;
    }
    let parsed = emptyMeta();
    try {
      parsed = parseEditorMeta(await store.load(documentKey));
    } catch {
      // A meta file we cannot read is not worth an error: the editor simply has no
      // saved inputs for this document.
    }
    if (seq !== loadSeq.current) return;
    setMeta(parsed);
  }, [store, documentKey]);

  // Load the file whenever the open document changes.
  useEffect(() => {
    namesRef.current = flowIdNames(doc);
    void refresh();
    // `doc` is deliberately not a dependency: this reloads per document, not per edit.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [refresh]);

  /**
   * Re-read when the host says something else wrote the file.
   *
   * Skipped while an edit of ours is still pending its debounced write: the file on
   * disk is a version behind by construction, and adopting it would undo the mock the
   * user just placed and then save the undo. The pending write lands within the
   * debounce and is itself announced, so nothing is lost by waiting.
   */
  const seenToken = useRef(reloadToken);
  useEffect(() => {
    if (reloadToken === seenToken.current) return;
    seenToken.current = reloadToken;
    if (dirtyRef.current) return;
    void refresh();
  }, [reloadToken, refresh]);

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

  /** Apply `fn` to the open document's entry for one flow, and mark the file dirty. */
  const edit = useCallback(
    (flowId: string, fn: (entry: FlowMeta) => FlowMeta) => {
      const flow = doc.flows.find((f) => f.id === flowId);
      if (!flow) return;
      const key = documentKey ?? "";
      setMeta((current) => {
        const file = fileMetaFor(current, key);
        const entry = file.flows[flow.name] ?? emptyFlowMeta();
        const flows = { ...file.flows, [flow.name]: fn(entry) };
        dirtyRef.current = true;
        return withFileMeta(current, key, { flows });
      });
    },
    [doc, documentKey],
  );

  /** The stored entry for one flow, or an empty one. */
  const entryOf = useCallback(
    (flowId: string): FlowMeta => {
      const flow = doc.flows.find((f) => f.id === flowId);
      if (!flow) return emptyFlowMeta();
      return fileMetaFor(meta, documentKey ?? "").flows[flow.name] ?? emptyFlowMeta();
    },
    [doc, meta, documentKey],
  );

  const value = useMemo<EditorMetaValue>(
    () => ({
      inputs: (flowId) => entryOf(flowId).inputs,
      canPersist,
      addInput(flowId, input) {
        const created: TestInput = { ...input, id: newId() };
        edit(flowId, (e) => ({ ...e, inputs: [...e.inputs, created] }));
        return created;
      },
      updateInput(flowId, input) {
        edit(flowId, (e) => ({
          ...e,
          inputs: e.inputs.map((i) => (i.id === input.id ? input : i)),
        }));
      },
      removeInput(flowId, inputId) {
        edit(flowId, (e) => ({ ...e, inputs: e.inputs.filter((i) => i.id !== inputId) }));
      },

      mockAt(flowId, address) {
        return entryOf(flowId).mocks?.find((m) => m.address === address);
      },
      setMock(flowId, mock) {
        edit(flowId, (e) => {
          const mocks = e.mocks ?? [];
          const at = mocks.findIndex((m) => m.address === mock.address);
          return {
            ...e,
            mocks: at < 0 ? [...mocks, mock] : mocks.map((m, i) => (i === at ? mock : m)),
          };
        });
      },
      removeMock(flowId, address) {
        edit(flowId, (e) => ({
          ...e,
          mocks: (e.mocks ?? []).filter((m) => m.address !== address),
        }));
      },

      spies: (flowId) => entryOf(flowId).spies ?? [],
      setSpy(flowId, address, on) {
        edit(flowId, (e) => {
          const spies = e.spies ?? [];
          const has = spies.includes(address);
          if (on === has) return e;
          return {
            ...e,
            spies: on ? [...spies, address] : spies.filter((s) => s !== address),
          };
        });
      },

      // The whole document's worth, for the run request. An address is rooted at a flow
      // name, so a mock or spy on another flow's block is not noise: the flow being
      // invoked may reach it through a flow-ref, and if it doesn't, the address simply
      // never fires. Sending everything means what the canvas shows is what the run does.
      enabledMocks() {
        const file = fileMetaFor(meta, documentKey ?? "");
        return Object.values(file.flows)
          .flatMap((e) => e.mocks ?? [])
          .filter((m) => m.enabled);
      },
      allSpies() {
        const file = fileMetaFor(meta, documentKey ?? "");
        return [...new Set(Object.values(file.flows).flatMap((e) => e.spies ?? []))];
      },
    }),
    [meta, documentKey, canPersist, edit, entryOf],
  );

  return (
    <EditorMetaContext.Provider value={value}>{children}</EditorMetaContext.Provider>
  );
}

/** The editor-meta capability. Null only when no provider is mounted. */
export function useEditorMeta(): EditorMetaValue | null {
  return useContext(EditorMetaContext);
}
