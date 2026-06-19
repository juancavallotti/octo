import { ReducerAction } from "@eetr/react-reducer-utils";
import { EditorDocument, blankDocument } from "@/app/model/document";
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
  SetEnvPayload,
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
} from "./actions";
import * as handlers from "./handlers";
import * as sourceHandlers from "./sourceHandlers";
import * as connectionHandlers from "./connectionHandlers";
import * as envHandlers from "./envHandlers";

/**
 * Editor-wide state. EditorShell is a "large" component, so its state lives in a
 * reducer (per the coding standards). The document is the in-memory editing model
 * (see app/model/document.ts); a file holds many flows, all editable at once.
 * `activeFlowId` is just the target for click-to-add and selection highlighting.
 * The pure state transitions live in handlers.ts; this file wires them to actions.
 */
export interface EditorState {
  document: EditorDocument;
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
}

function makeInitialState(): EditorState {
  return {
    document: blankDocument(),
    activeFlowId: null,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
    selectedComponentId: null,
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
    case EditorActionType.SET_ENV:
      return envHandlers.setEnv(state, action.data as SetEnvPayload);
    case EditorActionType.LOAD_DOCUMENT:
      return handlers.loadDocument(state, action.data as LoadDocumentPayload);
    case EditorActionType.SELECT_COMPONENT:
      return { ...state, selectedComponentId: action.data as string };
    case EditorActionType.CLEAR_SELECTION:
      return { ...state, selectedComponentId: null };
    default:
      return state;
  }
}
