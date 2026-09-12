"use client";

import { useEffect } from "react";
import { EditorActionType, useEditorState } from "../state/editorState";
import { isTypingTarget } from "./typing";

/**
 * Cmd/Ctrl+Z and its redo, bound for the whole editor rather than for the canvas:
 * undo is about the document, and the document is just as editable from the settings
 * panel or the resources view.
 *
 * Not bound while the user is typing. A text field has its own undo stack that the
 * browser maintains character by character, and taking Cmd+Z away from it to undo the
 * whole field edit is not what someone mid-word is asking for. Stepping out of the
 * field hands the shortcut back, by which point the whole edit is the right grain.
 */
export function useHistoryShortcuts(): void {
  const { dispatch } = useEditorState();

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey)) return;
      if (isTypingTarget(e.target)) return;

      const key = e.key.toLowerCase();
      // Ctrl+Y is redo on Windows, where Ctrl+Shift+Z is not the convention.
      if (key === "y" && !e.metaKey) {
        e.preventDefault();
        dispatch({ type: EditorActionType.REDO });
        return;
      }
      if (key !== "z") return;
      e.preventDefault();
      dispatch({ type: e.shiftKey ? EditorActionType.REDO : EditorActionType.UNDO });
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [dispatch]);
}
