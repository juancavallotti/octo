"use client";

import { Redo2, Undo2 } from "lucide-react";
import { EditorActionType, useEditorState } from "../state/editorState";
import { BAR_BUTTON } from "./barButton";

/**
 * Undo and redo, at the left of the document bar.
 *
 * The keyboard binding came first and is the one people who know it will use, but a
 * shortcut is only a feature for someone who already knows it is there — and undo is
 * the one command a person looks for before they trust a canvas enough to
 * experiment. Being visible is most of its value.
 *
 * Icon-only: they sit beside three labelled launchers, and two more words in a
 * nine-pixel-tall strip would crowd the file's own controls to say what a pair of
 * arrows already says.
 */
export default function HistoryButtons() {
  const { dispatch, canUndo, canRedo } = useEditorState();
  const shortcut = navigator?.platform?.startsWith("Mac") ? "⌘" : "Ctrl+";

  return (
    <>
      <button
        type="button"
        aria-label="Undo"
        title={`Undo (${shortcut}Z)`}
        // Disabled rather than hidden: a control that appears when it becomes usable
        // is one you cannot find when you need it, which for undo is exactly the
        // moment you are looking.
        disabled={!canUndo}
        onClick={() => dispatch({ type: EditorActionType.UNDO })}
        className={`${BAR_BUTTON} disabled:pointer-events-none disabled:opacity-35`}
      >
        <Undo2 size={14} />
      </button>
      <button
        type="button"
        aria-label="Redo"
        title={`Redo (${shortcut}⇧Z)`}
        disabled={!canRedo}
        onClick={() => dispatch({ type: EditorActionType.REDO })}
        className={`${BAR_BUTTON} disabled:pointer-events-none disabled:opacity-35`}
      >
        <Redo2 size={14} />
      </button>
      {/* The file's own controls are a different group from what you did to it. */}
      <span className="mx-1 h-4 w-px bg-black/10 dark:bg-white/10" aria-hidden />
    </>
  );
}
