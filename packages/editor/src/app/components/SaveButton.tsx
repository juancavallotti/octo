"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Save } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import {
  useFileSystem,
  type StoredDocument,
} from "../providers/FileSystemProvider";
import type { EditorDocument } from "../model/document";
import { toDefinitionYaml } from "../model/runConfig";

/**
 * Persists the current document through the filesystem capability. The first save
 * creates it; later saves update it. Unlike the RUN control it does not require a
 * valid document — a work in progress can be saved at any time. Save is disabled
 * only when there is nothing to save (an empty document) or nothing has changed
 * since the last save; an untitled document is persisted as "Untitled
 * integration". `onSaved` lets the host react to a save (e.g. promote the URL to
 * the newly created document) without coupling the editor to any app's routing.
 */
const DEFAULT_NAME = "Untitled integration";
export default function SaveButton({
  onSaved,
}: {
  onSaved?: (stored: StoredDocument) => void;
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
  const docEmpty =
    doc.flows.every((f) => !f.source && f.process.length === 0) &&
    doc.connectors.length === 0 &&
    doc.env.length === 0;
  const blocked = docEmpty || saved;

  const title = docEmpty
    ? "Nothing to save yet"
    : saved
      ? "No changes to save"
      : id
        ? "Save changes (⌘/Ctrl+S)"
        : "Save as a new integration (⌘/Ctrl+S)";

  const save = async () => {
    if (!fs) return;
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
      if (!id) {
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
  };

  // Cmd/Ctrl+S saves, mirroring the button's enabled state. The handler is kept
  // in a ref so the window listener registers once but always sees the latest
  // save closure and gate.
  const triggerRef = useRef<() => void>(() => {});
  useEffect(() => {
    triggerRef.current = () => {
      if (!busy && !blocked) void save();
    };
  });
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        triggerRef.current();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  // No filesystem capability => nothing to save against, so render nothing.
  if (!fs) return null;

  return (
    <div className="flex items-center gap-2">
      {error && <span className="text-xs text-red-500">{error}</span>}
      {saved && !error && (
        <span className="flex items-center gap-1 text-xs text-emerald-600">
          <Check size={13} /> Saved
        </span>
      )}
      <button
        type="button"
        onClick={save}
        disabled={busy || blocked}
        title={title}
        className="inline-flex items-center gap-1.5 rounded-md bg-sky-600 px-3 py-1 text-sm font-medium text-white hover:bg-sky-500 disabled:cursor-not-allowed disabled:opacity-40"
      >
        <Save className="h-3.5 w-3.5" />
        Save
      </button>
    </div>
  );
}
