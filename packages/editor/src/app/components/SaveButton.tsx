"use client";

import { Save } from "lucide-react";
import { useEditorState } from "../state/editorState";
import { useSave } from "../save/SaveContext";

/**
 * The Save control. All the persistence logic lives in the shared save
 * controller (SaveContext) so the button, the ⌘/Ctrl+S shortcut, and Enter in the
 * title field all drive one save; this component is just its button surface.
 * Renders nothing when there is no filesystem capability (no controller).
 *
 * It reports neither outcome. A successful save showed a "Saved" tick, which was
 * a state rather than a moment: it sat beside the button for as long as the
 * document went unedited, saying "nothing to save" next to a control offering to
 * save. The disabled button says that by itself.
 *
 * A failure goes to the Problems tab with the validation issues, where it is one
 * of the things standing between this document and a run — and where it has a
 * badge, a place to sit, and room for a sentence. See LogPanel.
 */
export default function SaveButton() {
  const ctl = useSave();
  const { state } = useEditorState();

  // No save controller => no filesystem capability => render nothing.
  if (!ctl) return null;

  const { save, busy, blocked, empty, saved } = ctl;
  const title = empty
    ? "Nothing to save yet"
    : saved
      ? "No changes to save"
      : state.integration.id
        ? "Save changes (⌘/Ctrl+S)"
        : "Save as a new integration (⌘/Ctrl+S)";

  return (
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
  );
}
