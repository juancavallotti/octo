import { blankDocument, emptyDocument } from "../model/document";
import { byPath, isConfigFileName, type ProjectFile } from "../model/project";
import type { EditorState } from "./reducer";
import type {
  CreateFilePayload,
  DeleteFilePayload,
  LoadProjectPayload,
  RenameFilePayload,
  SetProjectFolderPayload,
  SetProjectIdPayload,
  SetProjectTitlePayload,
  SwitchFilePayload,
} from "./actions";

/**
 * Pure state transitions for the open project: its metadata — the title, folder
 * membership and persisted id the title bar reads and Save writes — and its files.
 *
 * One file is active at a time. Its document is `state.document`, the thing every
 * edit and undo works on; the other files wait in `state.files`. Switching files
 * puts the active document back into its entry and takes the next one out, so an
 * unsaved edit survives the switch.
 */

/** The name a new project's first file takes: the conventional project file. */
export const FIRST_FILE = "octo.yaml";

/** The project's files with the active one as it is now, ordered by path. */
export function projectFiles(state: EditorState): ProjectFile[] {
  return byPath(
    state.files.map((f) => (f.path === state.activePath ? { ...f, document: state.document } : f)),
  );
}

/** Make `path` active, stashing the current active document first. */
function activate(state: EditorState, files: ProjectFile[], path: string): EditorState {
  const document = files.find((f) => f.path === path)?.document ?? blankDocument();
  return {
    ...state,
    files,
    activePath: path,
    document,
    activeFlowId: document.flows[0]?.id ?? null,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
  };
}

export function setProjectId(state: EditorState, p: SetProjectIdPayload): EditorState {
  return { ...state, project: { ...state.project, id: p.id } };
}

export function setProjectTitle(state: EditorState, p: SetProjectTitlePayload): EditorState {
  return { ...state, project: { ...state.project, name: p.name } };
}

export function setProjectFolder(state: EditorState, p: SetProjectFolderPayload): EditorState {
  return { ...state, project: { ...state.project, folderId: p.folderId } };
}

/**
 * Load a saved project: its metadata and every file together. The file opened is the
 * one asked for, else the first that holds a flow, else the first — a project file
 * of connections alone is not where anyone starts editing.
 */
export function loadProject(state: EditorState, p: LoadProjectPayload): EditorState {
  const files = byPath(p.files);
  const path =
    (p.activePath && files.some((f) => f.path === p.activePath) ? p.activePath : undefined) ??
    files.find((f) => f.document.flows.length > 0)?.path ??
    files[0]?.path ??
    FIRST_FILE;
  const withOne = files.length > 0 ? files : [{ path, document: emptyDocument(), declaresService: true }];
  return activate(
    { ...state, project: { id: p.id, name: p.name, folderId: p.folderId } },
    withOne,
    path,
  );
}

/** Reset to a fresh, unsaved project: one file holding one blank flow. */
export function newProject(state: EditorState): EditorState {
  const files = [{ path: FIRST_FILE, document: emptyDocument(), declaresService: true }];
  return activate({ ...state, project: { id: null, name: "", folderId: null } }, files, FIRST_FILE);
}

export function switchFile(state: EditorState, p: SwitchFilePayload): EditorState {
  if (p.path === state.activePath || !state.files.some((f) => f.path === p.path)) return state;
  return activate(state, projectFiles(state), p.path);
}

/** Add a config file and open it. A name the runtime would not load, or one taken, is refused. */
export function createFile(state: EditorState, p: CreateFilePayload): EditorState {
  if (!isConfigFileName(p.path) || state.files.some((f) => f.path === p.path)) return state;
  const files = [
    ...projectFiles(state),
    { path: p.path, document: emptyDocument(), declaresService: false },
  ];
  return activate(state, byPath(files), p.path);
}

export function renameFile(state: EditorState, p: RenameFilePayload): EditorState {
  if (
    p.from === p.to ||
    !isConfigFileName(p.to) ||
    !state.files.some((f) => f.path === p.from) ||
    state.files.some((f) => f.path === p.to)
  ) {
    return state;
  }
  const files = byPath(projectFiles(state).map((f) => (f.path === p.from ? { ...f, path: p.to } : f)));
  return activate(state, files, state.activePath === p.from ? p.to : state.activePath);
}

/** Remove a config file. The last one is kept: a project of no files is not one. */
export function deleteFile(state: EditorState, p: DeleteFilePayload): EditorState {
  if (state.files.length <= 1 || !state.files.some((f) => f.path === p.path)) return state;
  const files = projectFiles(state).filter((f) => f.path !== p.path);
  return activate(state, files, state.activePath === p.path ? files[0].path : state.activePath);
}
