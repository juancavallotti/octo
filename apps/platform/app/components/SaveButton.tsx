"use client";

import { useEffect, useRef, useState } from "react";
import { Check, Save } from "lucide-react";
import { useEditorState, EditorActionType } from "@/app/state/editorState";
import type { EditorDocument } from "@/app/model/document";
import { toDefinitionYaml } from "@/app/model/runConfig";
import {
  assignIntegration,
  createIntegration,
  updateIntegration,
} from "@/app/model/orchestrator";

/**
 * Persists the current document as an integration via the orchestrator. The
 * first save creates the row (and assigns any chosen folder); later saves update
 * it. Unlike the RUN control it does not require a valid document — a work in
 * progress can be saved at any time. Save is disabled only when there is nothing
 * to save (an empty document) or nothing has changed since the last save; an
 * untitled integration is persisted as "Untitled integration".
 */
const DEFAULT_NAME = "Untitled integration";
export default function SaveButton() {
  const { state, dispatch } = useEditorState();
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
    setBusy(true);
    setError(null);
    const saveName = name.trim() || DEFAULT_NAME;
    try {
      const definition = toDefinitionYaml(doc, saveName);
      if (id) {
        await updateIntegration(id, { name: saveName, definition });
      } else {
        const created = await createIntegration({
          name: saveName,
          definition,
        });
        if (folderId) await assignIntegration(folderId, created.id);
        dispatch({
          type: EditorActionType.SET_INTEGRATION_ID,
          data: { id: created.id },
        });
        // Promote the address bar to the bookmarkable URL without remounting the
        // editor (Next syncs the router for manual history updates).
        window.history.replaceState(null, "", `/i/${created.id}`);
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
