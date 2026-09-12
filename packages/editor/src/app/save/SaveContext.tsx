"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useEditorState, EditorActionType } from "../state/editorState";
import {
  useFileSystem,
  type StoredDocument,
} from "../providers/FileSystemProvider";
import type { EditorDocument } from "../model/document";
import { toDefinitionYaml } from "../model/runConfig";

/**
 * Shared save controller. Persisting the document is triggered from several
 * places — the Save button, the ⌘/Ctrl+S shortcut, and Enter in the title field —
 * so the logic lives here once and those consumers read it via `useSave()`. The
 * first save creates the document; later saves update it (and may re-id it when a
 * backend renames on save). Saving never requires a valid document — a work in
 * progress can be saved at any time — but is a no-op when there is nothing to save
 * (empty document) or nothing changed since the last save.
 *
 * `onSaved` lets the host react to a save (e.g. promote the URL to the newly
 * created/renamed document) without coupling the editor to any app's routing.
 */
const DEFAULT_NAME = "Untitled integration";

export interface SaveController {
  /**
   * Persist the current document; a no-op while busy or when nothing changed.
   *
   * `force` overrides only the "nothing worth persisting yet" guard, for a caller
   * that means the file itself: naming a brand-new flow is the user asking for a
   * file on disk, and refusing because they have not drawn anything yet leaves the
   * name they typed with nowhere to live.
   */
  save: (opts?: { force?: boolean }) => Promise<void>;
  busy: boolean;
  /**
   * Nothing to save: an empty document, no changes since the last save, or a
   * backend that declines writes.
   */
  blocked: boolean;
  /** Why the backend declines writes, when it does. */
  readOnly?: string;
  /** The document is empty (nothing worth persisting yet). */
  empty: boolean;
  /** The current document/name matches what was last saved. */
  saved: boolean;
  error: string | null;
}

const SaveContext = createContext<SaveController | null>(null);

/** The save controller, or null when there is no filesystem capability. */
export function useSave(): SaveController | null {
  return useContext(SaveContext);
}

export function SaveProvider({
  onSaved,
  children,
}: {
  onSaved?: (stored: StoredDocument) => void;
  children: ReactNode;
}) {
  const { state, dispatch } = useEditorState();
  const fs = useFileSystem();
  const { id, name, folderId } = state.integration;
  const doc = state.document;

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  // Snapshot of what was last saved; the "Saved" note shows only while the
  // current document/name/folder still match it (no effect needed — editing
  // produces a fresh document reference, which clears the match).
  const [savedSnapshot, setSavedSnapshot] = useState<{
    doc: EditorDocument;
    name: string;
    folderId: string | null;
  } | null>(null);
  const saved =
    savedSnapshot !== null &&
    savedSnapshot.doc === doc &&
    savedSnapshot.name === name &&
    savedSnapshot.folderId === folderId;

  // "Empty" = nothing worth persisting yet: no flow has a source or a step, and
  // there are no connections or env vars.
  const empty =
    doc.flows.every((f) => !f.source && f.process.length === 0) &&
    doc.connectors.length === 0 &&
    doc.env.length === 0;
  const readOnly = fs?.readOnly;
  const blocked = empty || saved || Boolean(readOnly);

  const save = useCallback(async ({ force = false } = {}) => {
    if (!fs || fs.readOnly || busy || saved) return;
    if (empty && !force) return;
    setBusy(true);
    setError(null);
    const saveName = name.trim() || DEFAULT_NAME;
    try {
      const definition = toDefinitionYaml(doc, saveName);
      const stored = await fs.save(id || null, {
        name: saveName,
        definition,
        folderId,
      });
      // Adopt the persisted id whenever it changes: on the first save (null → new
      // id) and when a save renames the record (e.g. the standalone derives the
      // filename from the name, so renaming the flow renames — and re-ids — it).
      if (stored.id !== id) {
        dispatch({
          type: EditorActionType.SET_INTEGRATION_ID,
          data: { id: stored.id },
        });
      }
      // Reflect a defaulted name in the title field so the UI matches what was
      // stored (and so the saved-snapshot comparison holds).
      if (saveName !== name) {
        dispatch({
          type: EditorActionType.SET_INTEGRATION_TITLE,
          data: { name: saveName },
        });
      }
      setSavedSnapshot({ doc, name: saveName, folderId });
      // Let the host promote its URL / reflect the (possibly new) id.
      onSaved?.(stored);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  }, [fs, busy, empty, saved, name, doc, id, folderId, dispatch, onSaved]);

  // Cmd/Ctrl+S saves. The handler is kept in a ref so the window listener
  // registers once but always sees the latest save closure.
  const saveRef = useRef(save);
  useEffect(() => {
    saveRef.current = save;
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        void saveRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // No filesystem capability => no save controller (Save/Enter render/do nothing).
  const value: SaveController | null = fs
    ? { save, busy, blocked, empty, saved, error, readOnly }
    : null;

  return <SaveContext.Provider value={value}>{children}</SaveContext.Provider>;
}
