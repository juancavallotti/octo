import { ReducerAction } from "@eetr/react-reducer-utils";
import { EditorDocument, blankDocument } from "../model/document";
import {
  AddBlockPayload,
  AddSourcePayload,
  EditorActionType,
  LoadDocumentPayload,
  MoveBlockAcrossPayload,
  MoveBlockPayload,
  RemoveBlockPayload,
  RemoveFlowPayload,
  RemoveSourcePayload,
  RenameBlockPayload,
  RenameFlowPayload,
  AddSlotFlowPayload,
  RemoveSlotFlowPayload,
  SetFlowMetaPayload,
  SetEnvPayload,
  SetResourcesPayload,
  SelectBlockPayload,
  SelectSourcePayload,
  SetActiveFlowPayload,
  UpdateBlockSettingPayload,
  UpdateSourceSettingPayload,
  UpdateSourceConnectorPayload,
  AddConnectionPayload,
  RemoveConnectionPayload,
  RenameConnectionPayload,
  SelectConnectionPayload,
  UpdateConnectionSettingPayload,
  SetProjectIdPayload,
  SetProjectTitlePayload,
  SetProjectFolderPayload,
  LoadProjectPayload,
  SwitchFilePayload,
  CreateFilePayload,
  RenameFilePayload,
  DeleteFilePayload,
  SetFlowsViewPayload,
  NewItemPayload,
  PendingNew,
  SetViewModePayload,
} from "./actions";
import * as handlers from "./handlers";
import * as sourceHandlers from "./sourceHandlers";
import * as connectionHandlers from "./connectionHandlers";
import * as envHandlers from "./envHandlers";
import * as slotHandlers from "./slotHandlers";
import * as projectHandlers from "./projectHandlers";
import type { ProjectFile } from "../model/project";

/**
 * Editor-wide state. EditorRoot is a "large" component, so its state lives in a
 * reducer (per the coding standards). The document is the in-memory editing model
 * (see app/model/document.ts); a file holds many flows, all editable at once.
 * `activeFlowId` is just the target for click-to-add and selection highlighting.
 * The pure state transitions live in handlers.ts; this file wires them to actions.
 */
/**
 * The persisted-project metadata the title bar binds to. `id` is null until the
 * project has been saved at least once; `folderId` is null when unfiled.
 */
export interface ProjectMeta {
  id: string | null;
  name: string;
  folderId: string | null;
}

/** Which view the editor body shows: the flows, the resources, or the test suites. */
export type ViewMode = SetViewModePayload;

/** How the flows view shows the open file: the visual canvas or the read-only YAML. */
export type FlowsView = SetFlowsViewPayload;

export interface EditorState {
  /** The active file's document: what the canvas shows and every edit changes. */
  document: EditorDocument;
  /**
   * Every config file of the project. The active file's entry is as it was when it
   * was opened; its current state is `document`. Read the project through
   * `projectFiles(state)`, never this list directly.
   */
  files: ProjectFile[];
  /** The path of the file `document` belongs to. */
  activePath: string;
  /** Editor body view: the flows, the resources, or the test suites. */
  viewMode: ViewMode;
  /** How the flows view shows the open file, kept while another view is open. */
  flowsView: FlowsView;
  /** A "new" waiting for the view that makes it to begin it; see NEW_ITEM. */
  pendingNew: PendingNew | null;
  /** Target flow for click-to-add; also highlighted on the canvas. */
  activeFlowId: string | null;
  /** Currently selected block on the canvas, or null. */
  selectedBlockId: string | null;
  /** Flow whose source is currently selected (for source settings), or null. */
  selectedSourceFlowId: string | null;
  /** Currently selected connection (for connection settings), or null. */
  selectedConnectionId: string | null;
  /** Currently highlighted palette component id, or null. */
  selectedComponentId: string | null;
  /** Metadata for the project the files belong to (when saving is enabled). */
  project: ProjectMeta;
}

function makeInitialState(): EditorState {
  const document = blankDocument();
  return {
    document,
    files: [{ path: projectHandlers.FIRST_FILE, document, declaresService: true }],
    activePath: projectHandlers.FIRST_FILE,
    viewMode: "flows",
    flowsView: "canvas",
    pendingNew: null,
    activeFlowId: null,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
    selectedComponentId: null,
    project: { id: null, name: "", folderId: null },
  };
}

export const initialState: EditorState = makeInitialState();

export function reducer(
  state: EditorState = initialState,
  action: ReducerAction<EditorActionType>,
): EditorState {
  switch (action.type) {
    case EditorActionType.ADD_FLOW:
      return handlers.addFlow(state);
    case EditorActionType.ADD_BLOCK:
      return handlers.addBlock(state, action.data as AddBlockPayload);
    case EditorActionType.MOVE_BLOCK:
      return handlers.moveBlock(state, action.data as MoveBlockPayload);
    case EditorActionType.MOVE_BLOCK_ACROSS:
      return handlers.moveBlockAcross(
        state,
        action.data as MoveBlockAcrossPayload,
      );
    case EditorActionType.REMOVE_BLOCK:
      return handlers.removeBlock(state, action.data as RemoveBlockPayload);
    case EditorActionType.REMOVE_FLOW:
      return handlers.removeFlow(state, action.data as RemoveFlowPayload);
    case EditorActionType.RENAME_FLOW:
      return handlers.renameFlow(state, action.data as RenameFlowPayload);
    case EditorActionType.SELECT_BLOCK:
      return {
        ...state,
        selectedBlockId: (action.data as SelectBlockPayload).blockId,
        selectedSourceFlowId: null,
        selectedConnectionId: null,
      };
    case EditorActionType.UPDATE_BLOCK_SETTING:
      return handlers.updateBlockSetting(
        state,
        action.data as UpdateBlockSettingPayload,
      );
    case EditorActionType.RENAME_BLOCK:
      return handlers.renameBlock(state, action.data as RenameBlockPayload);
    case EditorActionType.SET_ACTIVE_FLOW:
      return {
        ...state,
        activeFlowId: (action.data as SetActiveFlowPayload).flowId,
        selectedBlockId: null,
        selectedSourceFlowId: null,
        selectedConnectionId: null,
      };
    case EditorActionType.ADD_SOURCE:
      return sourceHandlers.addSource(state, action.data as AddSourcePayload);
    case EditorActionType.SELECT_SOURCE:
      return sourceHandlers.selectSource(
        state,
        action.data as SelectSourcePayload,
      );
    case EditorActionType.UPDATE_SOURCE_SETTING:
      return sourceHandlers.updateSourceSetting(
        state,
        action.data as UpdateSourceSettingPayload,
      );
    case EditorActionType.UPDATE_SOURCE_CONNECTOR:
      return sourceHandlers.updateSourceConnector(
        state,
        action.data as UpdateSourceConnectorPayload,
      );
    case EditorActionType.REMOVE_SOURCE:
      return sourceHandlers.removeSource(
        state,
        action.data as RemoveSourcePayload,
      );
    case EditorActionType.ADD_CONNECTION:
      return connectionHandlers.addConnection(
        state,
        action.data as AddConnectionPayload,
      );
    case EditorActionType.SELECT_CONNECTION:
      return connectionHandlers.selectConnection(
        state,
        action.data as SelectConnectionPayload,
      );
    case EditorActionType.RENAME_CONNECTION:
      return connectionHandlers.renameConnection(
        state,
        action.data as RenameConnectionPayload,
      );
    case EditorActionType.UPDATE_CONNECTION_SETTING:
      return connectionHandlers.updateConnectionSetting(
        state,
        action.data as UpdateConnectionSettingPayload,
      );
    case EditorActionType.REMOVE_CONNECTION:
      return connectionHandlers.removeConnection(
        state,
        action.data as RemoveConnectionPayload,
      );
    case EditorActionType.ADD_SLOT_FLOW:
      return slotHandlers.addSlotFlow(state, action.data as AddSlotFlowPayload);
    case EditorActionType.REMOVE_SLOT_FLOW:
      return slotHandlers.removeSlotFlow(
        state,
        action.data as RemoveSlotFlowPayload,
      );
    case EditorActionType.SET_FLOW_META:
      return slotHandlers.setFlowMeta(state, action.data as SetFlowMetaPayload);
    case EditorActionType.SET_ENV:
      return envHandlers.setEnv(state, action.data as SetEnvPayload);
    case EditorActionType.SET_RESOURCES:
      return envHandlers.setResources(
        state,
        action.data as SetResourcesPayload,
      );
    case EditorActionType.LOAD_DOCUMENT:
      return handlers.loadDocument(state, action.data as LoadDocumentPayload);
    case EditorActionType.SET_PROJECT_ID:
      return projectHandlers.setProjectId(state, action.data as SetProjectIdPayload);
    case EditorActionType.SET_PROJECT_TITLE:
      return projectHandlers.setProjectTitle(state, action.data as SetProjectTitlePayload);
    case EditorActionType.SET_PROJECT_FOLDER:
      return projectHandlers.setProjectFolder(state, action.data as SetProjectFolderPayload);
    case EditorActionType.LOAD_PROJECT:
      return projectHandlers.loadProject(state, action.data as LoadProjectPayload);
    case EditorActionType.NEW_PROJECT:
      return projectHandlers.newProject(state);
    case EditorActionType.SWITCH_FILE:
      return projectHandlers.switchFile(state, action.data as SwitchFilePayload);
    case EditorActionType.CREATE_FILE:
      return projectHandlers.createFile(state, action.data as CreateFilePayload);
    case EditorActionType.RENAME_FILE:
      return projectHandlers.renameFile(state, action.data as RenameFilePayload);
    case EditorActionType.DELETE_FILE:
      return projectHandlers.deleteFile(state, action.data as DeleteFilePayload);
    case EditorActionType.SET_VIEW_MODE:
      return { ...state, viewMode: action.data as SetViewModePayload };
    case EditorActionType.SET_FLOWS_VIEW:
      return { ...state, flowsView: action.data as SetFlowsViewPayload };
    case EditorActionType.NEW_ITEM:
      return handlers.newItem(state, action.data as NewItemPayload);
    case EditorActionType.CONSUME_NEW_ITEM:
      return { ...state, pendingNew: null };
    case EditorActionType.SELECT_COMPONENT:
      return { ...state, selectedComponentId: action.data as string };
    case EditorActionType.CLEAR_SELECTION:
      return { ...state, selectedComponentId: null };
    default:
      return state;
  }
}
