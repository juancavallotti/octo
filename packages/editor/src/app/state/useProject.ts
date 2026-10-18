import { useCallback, useMemo } from "react";
import type { EditorDocument } from "../model/document";
import { fileOfNode, projectView, type ProjectFile } from "../model/project";
import { EditorActionType, useEditorState } from "./editorState";
import { projectFiles } from "./projectHandlers";

/**
 * The open project, for components. Anything that resolves a name — a connection, a
 * flow-ref target, a template, an env variable — reads the project view, because the
 * runtime resolves names across every file; anything that edits reads
 * `state.document`, the active file.
 */

/** Every file of the project, the active one as it is now. */
export function useProjectFiles(): ProjectFile[] {
  const { files, activePath, document } = useEditorState().state;
  return useMemo(
    () => projectFiles({ files, activePath, document }),
    [files, activePath, document],
  );
}

/** The project's files merged, as the runtime will see them. */
export function useProjectView(): EditorDocument {
  const files = useProjectFiles();
  return useMemo(() => projectView(files), [files]);
}

/**
 * Open the file that holds a node — a flow, a block or a connection — so that a
 * selection made from a project-wide list (a problem, a connection) lands on
 * something the canvas shows. A no-op for a node already in the open file.
 */
export function useRevealNode(): (id: string) => void {
  const { state, dispatch } = useEditorState();
  const files = useProjectFiles();
  const { activePath } = state;
  return useCallback(
    (id: string) => {
      const path = fileOfNode(files, id);
      if (path && path !== activePath) {
        dispatch({ type: EditorActionType.SWITCH_FILE, data: { path } });
      }
    },
    [files, activePath, dispatch],
  );
}
