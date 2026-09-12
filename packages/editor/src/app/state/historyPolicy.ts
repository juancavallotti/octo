import type { ReducerAction } from "@eetr/react-reducer-utils";
import { EditorActionType } from "./actions";
import type {
  RenameBlockPayload,
  RenameFlowPayload,
  SetFlowMetaPayload,
  UpdateBlockSettingPayload,
  UpdateConnectionSettingPayload,
  UpdateSourceSettingPayload,
} from "./actions";
import type { HistoryPolicy, Tracking } from "./history";

/**
 * Which editor actions undo remembers.
 *
 * A table rather than a rule, because the rule everyone reaches for first — "record
 * anything that touched the document" — cannot be evaluated from the action alone,
 * and evaluating it from the result would make undo step through selection changes
 * that happen to arrive bundled with an edit.
 *
 * The table is exhaustive over EditorActionType and the switch has no default, so a
 * new action fails the type-check here rather than silently defaulting to a behaviour
 * nobody chose.
 */

type Action = ReducerAction<EditorActionType>;

export function trackingOf(type: EditorActionType): Tracking {
  switch (type) {
    // Everything that changes the document.
    case EditorActionType.ADD_FLOW:
    case EditorActionType.ADD_BLOCK:
    case EditorActionType.MOVE_BLOCK:
    case EditorActionType.MOVE_BLOCK_ACROSS:
    case EditorActionType.REMOVE_BLOCK:
    case EditorActionType.REMOVE_FLOW:
    case EditorActionType.RENAME_FLOW:
    case EditorActionType.UPDATE_BLOCK_SETTING:
    case EditorActionType.RENAME_BLOCK:
    case EditorActionType.ADD_SOURCE:
    case EditorActionType.UPDATE_SOURCE_SETTING:
    case EditorActionType.UPDATE_SOURCE_CONNECTOR:
    case EditorActionType.REMOVE_SOURCE:
    case EditorActionType.ADD_CONNECTION:
    case EditorActionType.RENAME_CONNECTION:
    case EditorActionType.UPDATE_CONNECTION_SETTING:
    case EditorActionType.REMOVE_CONNECTION:
    case EditorActionType.ADD_SLOT_FLOW:
    case EditorActionType.REMOVE_SLOT_FLOW:
    case EditorActionType.SET_FLOW_META:
    case EditorActionType.SET_ENV:
    case EditorActionType.SET_RESOURCES:
      return "record";

    // The view, the selection, and which integration the document maps to. None of
    // these is an edit, and undo stepping through them would mean pressing Cmd+Z
    // three times to take back one change.
    case EditorActionType.SELECT_BLOCK:
    case EditorActionType.SELECT_SOURCE:
    case EditorActionType.SELECT_CONNECTION:
    case EditorActionType.SET_ACTIVE_FLOW:
    case EditorActionType.SET_VIEW_MODE:
    case EditorActionType.SELECT_COMPONENT:
    case EditorActionType.CLEAR_SELECTION:
    case EditorActionType.SET_INTEGRATION_ID:
    case EditorActionType.SET_INTEGRATION_TITLE:
    case EditorActionType.SET_INTEGRATION_FOLDER:
      return "skip";

    // A different document is now open. Undoing into the previous one would silently
    // overwrite the file the user just switched to.
    case EditorActionType.LOAD_DOCUMENT:
    case EditorActionType.LOAD_INTEGRATION:
    case EditorActionType.NEW_INTEGRATION:
      return "reset";

    // Handled by the history wrapper; they never reach the inner reducer.
    case EditorActionType.UNDO:
    case EditorActionType.REDO:
      return "skip";
  }
}

/**
 * What edit an action continues, so that typing a name or a setting is one undo step
 * instead of one per keystroke. Keyed by target *and* field: moving to the next field
 * starts a new step, which is the granularity a user expects to get back.
 */
export function coalesceKey(action: Action): string | null {
  switch (action.type) {
    case EditorActionType.UPDATE_BLOCK_SETTING: {
      const p = action.data as UpdateBlockSettingPayload;
      return `block-setting:${p.blockId}:${p.field}`;
    }
    case EditorActionType.UPDATE_SOURCE_SETTING: {
      const p = action.data as UpdateSourceSettingPayload;
      return `source-setting:${p.flowId}:${p.field}`;
    }
    case EditorActionType.UPDATE_CONNECTION_SETTING: {
      const p = action.data as UpdateConnectionSettingPayload;
      return `connection-setting:${p.id}:${p.field}`;
    }
    case EditorActionType.SET_FLOW_META: {
      const p = action.data as SetFlowMetaPayload;
      return `flow-meta:${p.flowId}:${p.field}`;
    }
    case EditorActionType.RENAME_BLOCK:
      return `rename-block:${(action.data as RenameBlockPayload).blockId}`;
    case EditorActionType.RENAME_FLOW:
      return `rename-flow:${(action.data as RenameFlowPayload).flowId}`;
    default:
      return null;
  }
}

export const EDITOR_HISTORY: HistoryPolicy<Action> = {
  track: (action) => trackingOf(action.type),
  isUndo: (action) => action.type === EditorActionType.UNDO,
  isRedo: (action) => action.type === EditorActionType.REDO,
  coalesceKey,
};
