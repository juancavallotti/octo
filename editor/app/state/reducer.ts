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
  RenameBlockPayload,
  SelectBlockPayload,
  SetActiveFlowPayload,
  UpdateBlockSettingPayload,
} from "./actions";
import * as handlers from "./handlers";

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
  /** Currently highlighted palette component id, or null. */
  selectedComponentId: string | null;
}

function makeInitialState(): EditorState {
  return {
    document: blankDocument(),
    activeFlowId: null,
    selectedBlockId: null,
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
    case EditorActionType.SELECT_BLOCK:
      return {
        ...state,
        selectedBlockId: (action.data as SelectBlockPayload).blockId,
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
      };
    case EditorActionType.ADD_SOURCE:
      return handlers.addSource(state, action.data as AddSourcePayload);
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
