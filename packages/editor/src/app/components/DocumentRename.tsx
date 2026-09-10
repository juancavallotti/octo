"use client";

import { useRef, useState } from "react";
import { Pencil } from "lucide-react";
import { useEditorState, EditorActionType } from "../state/editorState";
import { useSave } from "../save/SaveContext";
import { BAR_BUTTON } from "./barButton";

/**
 * The document's name in the bar, and the pencil that renames it.
 *
 * `children` is whatever the host shows when it is not being renamed — the
 * standalone's file switcher, a plain chip in the platform — because the two
 * differ only in what else you can do with the name once it is on screen. The
 * field replaces that rather than sitting beside it: two names at once reads as
 * two different documents.
 *
 * Renaming is an edit plus a save. Edits go straight into editor state (rather
 * than into local state committed at the end), which is what keeps the save
 * controller from reading a stale name when Enter asks it to write. The save is
 * forced: naming a document that has not been drawn yet is still a request to
 * store it, and the controller's "nothing worth persisting" rule is about the
 * Save button.
 */
export default function DocumentRename({
  children,
  placeholder = "untitled",
  label = "Rename",
}: {
  children: React.ReactNode;
  /** Shown in the empty field, e.g. "untitled-file". */
  placeholder?: string;
  /** Accessible name for the pencil and the field. */
  label?: string;
}) {
  const { state, dispatch } = useEditorState();
  const save = useSave();
  const [renaming, setRenaming] = useState(false);
  // What the name was when the rename started, so Escape can put it back.
  const before = useRef(state.integration.name);

  const rename = (name: string) =>
    dispatch({ type: EditorActionType.SET_INTEGRATION_TITLE, data: { name } });

  const commit = () => {
    setRenaming(false);
    if (!state.integration.name.trim()) rename(before.current);
    else void save?.save({ force: true });
  };

  return (
    <div className="flex items-center">
      {renaming ? (
        <input
          type="text"
          autoFocus
          aria-label={label}
          value={state.integration.name}
          placeholder={placeholder}
          onChange={(e) => rename(e.target.value)}
          onBlur={commit}
          onFocus={(e) => e.currentTarget.select()}
          onKeyDown={(e) => {
            if (e.key === "Enter") e.currentTarget.blur();
            if (e.key === "Escape") {
              rename(before.current);
              setRenaming(false);
            }
          }}
          className="w-48 rounded-md border border-black/20 bg-transparent px-2 py-1 text-[13px] outline-none dark:border-white/25"
        />
      ) : (
        children
      )}

      <button
        type="button"
        aria-label={label}
        onClick={() => {
          before.current = state.integration.name;
          setRenaming(true);
        }}
        className={`${BAR_BUTTON} px-1`}
      >
        <Pencil size={13} className="text-zinc-400" />
      </button>
    </div>
  );
}
