"use client";

import { useEffect } from "react";
import { owningFlowId } from "../model/document";
import { EditorActionType, useEditorState } from "../state/editorState";
import { isTypingTarget } from "./typing";

/**
 * Delete removes what is selected; Escape deselects.
 *
 * Selection in the reducer is four mutually exclusive ids, so "what is selected" has
 * one answer and Delete has one meaning — which is why this reads as a chain rather
 * than a switch over some selection kind.
 *
 * Bound in the bubble phase deliberately. A popover and the CEL completion menu both
 * stop Escape from propagating while they are open (see components/ui/Popover.tsx and
 * cel/useCelCompletion.ts), so the first Escape closes the thing in front of the user
 * and never reaches here. Capturing would take that away from them.
 */
export function useSelectionShortcuts(): void {
  const { state, dispatch } = useEditorState();
  const { document: doc, selectedBlockId, selectedConnectionId, selectedSourceFlowId } = state;

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (isTypingTarget(e.target)) return;

      if (e.key === "Escape") {
        if (!selectedBlockId && !selectedConnectionId && !selectedSourceFlowId) return;
        // SELECT_BLOCK(null) is the reducer's own way of clearing all three.
        dispatch({ type: EditorActionType.SELECT_BLOCK, data: { blockId: null } });
        return;
      }

      if (e.key !== "Delete" && e.key !== "Backspace") return;
      // A modifier makes this someone else's shortcut, not a delete.
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      if (selectedBlockId) {
        const flowId = owningFlowId(doc, selectedBlockId);
        // A selection pointing at a block that is no longer in the document is stale;
        // deleting nothing is better than guessing which flow was meant.
        if (!flowId) return;
        e.preventDefault();
        dispatch({
          type: EditorActionType.REMOVE_BLOCK,
          data: { flowId, blockId: selectedBlockId },
        });
        return;
      }
      if (selectedConnectionId) {
        e.preventDefault();
        dispatch({ type: EditorActionType.REMOVE_CONNECTION, data: { id: selectedConnectionId } });
        return;
      }
      if (selectedSourceFlowId) {
        e.preventDefault();
        dispatch({
          type: EditorActionType.REMOVE_SOURCE,
          data: { flowId: selectedSourceFlowId },
        });
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [doc, selectedBlockId, selectedConnectionId, selectedSourceFlowId, dispatch]);
}
