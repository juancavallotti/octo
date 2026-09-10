"use client";

import { Check, Save } from "lucide-react";
import { useEditorState } from "../state/editorState";
import { useSave } from "../save/SaveContext";

/**
 * The Save control. All the persistence logic lives in the shared save
 * controller (SaveContext) so the button, the ⌘/Ctrl+S shortcut, and Enter in the
 * title field all drive one save; this component is just its button surface.
 * Renders nothing when there is no filesystem capability (no controller).
 */
export default function SaveButton() {
  const ctl = useSave();
  const { state } = useEditorState();

  // No save controller => no filesystem capability => render nothing.
  if (!ctl) return null;

  const { save, busy, blocked, empty, saved, error } = ctl;
  const title = empty
    ? "Nothing to save yet"
    : saved
      ? "No changes to save"
      : state.integration.id
        ? "Save changes (⌘/Ctrl+S)"
        : "Save as a new integration (⌘/Ctrl+S)";

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
        // Wrapped rather than passed: save() takes options now, and a click event
        // is not one of them.
        onClick={() => void save()}
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
