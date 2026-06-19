"use client";

import { useState } from "react";
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
 * progress can be saved at any time — but a name is still required since the
 * orchestrator rejects empty names.
 */
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

  const trimmedName = name.trim();
  const blocked = trimmedName === "";

  const title = blocked
    ? "Name the integration to save"
    : id
      ? "Save changes"
      : "Save as a new integration";

  const save = async () => {
    setBusy(true);
    setError(null);
    try {
      const definition = toDefinitionYaml(doc, trimmedName);
      if (id) {
        await updateIntegration(id, { name: trimmedName, definition });
      } else {
        const created = await createIntegration({
          name: trimmedName,
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
      setSavedSnapshot({ doc, name, folderId });
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setBusy(false);
    }
  };

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
