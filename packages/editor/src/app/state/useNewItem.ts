import { useCallback } from "react";
import { EditorActionType, useEditorState } from "./editorState";
import type { NewItemPayload } from "./actions";

/** What a host can ask the editor to start a new one of. */
export type NewItemKind = "flow" | NewItemPayload;

/**
 * Start something new from outside the editor's own controls — a host's menu. A flow
 * is added to the open file at once and shown on the canvas, as one undoable edit;
 * a config file, resource or test suite needs a name or a choice, so its view opens
 * and begins one (NEW_ITEM).
 */
export function useNewItem(): (kind: NewItemKind) => void {
  const { dispatch } = useEditorState();
  return useCallback(
    (kind: NewItemKind) => {
      if (kind !== "flow") {
        dispatch({ type: EditorActionType.NEW_ITEM, data: kind });
        return;
      }
      dispatch({ type: EditorActionType.SET_VIEW_MODE, data: "flows" });
      dispatch({ type: EditorActionType.SET_FLOWS_VIEW, data: "canvas" });
      dispatch({ type: EditorActionType.ADD_FLOW });
    },
    [dispatch],
  );
}
