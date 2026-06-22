import type { EditorDocument, EnvVar } from "@/app/model/document";

/**
 * Editor reducer actions. The payload travels on the action's `data` field (per
 * @eetr/react-reducer-utils' ReducerAction). The payload types below document
 * what `data` holds for each action.
 */
export enum EditorActionType {
  /** Add an empty flow to the document and make it active. */
  ADD_FLOW = "ADD_FLOW",
  /** Append (or insert at `index`) a new block into a flow (active by default). */
  ADD_BLOCK = "ADD_BLOCK",
  /** Reorder a block within a flow's process chain. */
  MOVE_BLOCK = "MOVE_BLOCK",
  /** Move a block from one flow to another (possibly nested) at an index. */
  MOVE_BLOCK_ACROSS = "MOVE_BLOCK_ACROSS",
  /** Remove a block from a flow by id. */
  REMOVE_BLOCK = "REMOVE_BLOCK",
  /** Remove a top-level flow from the document by id. */
  REMOVE_FLOW = "REMOVE_FLOW",
  /** Rename a flow (by id). */
  RENAME_FLOW = "RENAME_FLOW",
  /** Mark a canvas block as selected (or clear with null). */
  SELECT_BLOCK = "SELECT_BLOCK",
  /** Update one setting field of a block (by id). */
  UPDATE_BLOCK_SETTING = "UPDATE_BLOCK_SETTING",
  /** Rename a block's step name (by id). */
  RENAME_BLOCK = "RENAME_BLOCK",
  /** Switch which flow is active (the target for click-to-add). */
  SET_ACTIVE_FLOW = "SET_ACTIVE_FLOW",
  /** Give a flow a source of the chosen connector/type and select it. */
  ADD_SOURCE = "ADD_SOURCE",
  /** Mark a flow's source as selected (or clear with null). */
  SELECT_SOURCE = "SELECT_SOURCE",
  /** Update one setting field of a flow's source. */
  UPDATE_SOURCE_SETTING = "UPDATE_SOURCE_SETTING",
  /** Bind a flow's source to a connector instance (by name), or clear it. */
  UPDATE_SOURCE_CONNECTOR = "UPDATE_SOURCE_CONNECTOR",
  /** Remove a flow's source. */
  REMOVE_SOURCE = "REMOVE_SOURCE",
  /** Add a connector instance ("connection") of the chosen type and select it. */
  ADD_CONNECTION = "ADD_CONNECTION",
  /** Mark a connection as selected (or clear with null). */
  SELECT_CONNECTION = "SELECT_CONNECTION",
  /** Rename a connection (by id). The name is the slug-style reference. */
  RENAME_CONNECTION = "RENAME_CONNECTION",
  /** Update one setting field of a connection. */
  UPDATE_CONNECTION_SETTING = "UPDATE_CONNECTION_SETTING",
  /** Remove a connection (by id). */
  REMOVE_CONNECTION = "REMOVE_CONNECTION",
  /** Append a sub-flow to a composite block's list slot (switch case / fork branch). */
  ADD_SLOT_FLOW = "ADD_SLOT_FLOW",
  /** Remove a sub-flow from a composite block's list slot by id. */
  REMOVE_SLOT_FLOW = "REMOVE_SLOT_FLOW",
  /** Set a sub-flow's per-entry metadata: a case's `when`, a route/tool's description, a tool's inputSchema. */
  SET_FLOW_META = "SET_FLOW_META",
  /** Replace the document's declared environment variables. */
  SET_ENV = "SET_ENV",
  /** Replace the whole document (file load or "new"). */
  LOAD_DOCUMENT = "LOAD_DOCUMENT",
  /** Record the persisted id after the integration is first saved. */
  SET_INTEGRATION_ID = "SET_INTEGRATION_ID",
  /** Set the current integration's display title. */
  SET_INTEGRATION_TITLE = "SET_INTEGRATION_TITLE",
  /** Set the current integration's folder (null = unfiled). */
  SET_INTEGRATION_FOLDER = "SET_INTEGRATION_FOLDER",
  /** Load a saved integration: replace the document and set its metadata. */
  LOAD_INTEGRATION = "LOAD_INTEGRATION",
  /** Start a fresh, unsaved integration (clear metadata + document). */
  NEW_INTEGRATION = "NEW_INTEGRATION",
  /** Highlight a palette component. */
  SELECT_COMPONENT = "SELECT_COMPONENT",
  /** Clear the palette highlight. */
  CLEAR_SELECTION = "CLEAR_SELECTION",
}

export interface AddBlockPayload {
  blockType: string;
  /** Target flow; defaults to the active flow when omitted. */
  flowId?: string;
  index?: number;
}

export interface MoveBlockPayload {
  flowId: string;
  fromIndex: number;
  toIndex: number;
}

export interface MoveBlockAcrossPayload {
  fromFlowId: string;
  toFlowId: string;
  blockId: string;
  /** Insertion index in the target flow; appends when omitted. */
  index?: number;
}

export interface RemoveBlockPayload {
  flowId: string;
  blockId: string;
}

export interface RemoveFlowPayload {
  flowId: string;
}

export interface RenameFlowPayload {
  flowId: string;
  name: string;
}

export interface SelectBlockPayload {
  blockId: string | null;
}

export interface UpdateBlockSettingPayload {
  blockId: string;
  field: string;
  value: unknown;
}

export interface RenameBlockPayload {
  blockId: string;
  name: string;
}

export interface SetActiveFlowPayload {
  flowId: string;
}

export interface AddSourcePayload {
  flowId: string;
  /** Connector type that exposes the source (stored on the source node). */
  connector: string;
  /** Source type within that connector. */
  type: string;
}

export interface SelectSourcePayload {
  flowId: string | null;
}

export interface UpdateSourceSettingPayload {
  flowId: string;
  field: string;
  value: unknown;
}

export interface UpdateSourceConnectorPayload {
  flowId: string;
  /** Connector instance name to bind, or undefined to use the default connector. */
  connector: string | undefined;
}

export interface RemoveSourcePayload {
  flowId: string;
}

export interface AddConnectionPayload {
  /** Connector type to instantiate (matches a schema ConnectorSpec). */
  type: string;
}

export interface SelectConnectionPayload {
  id: string | null;
}

export interface RenameConnectionPayload {
  id: string;
  name: string;
}

export interface UpdateConnectionSettingPayload {
  id: string;
  field: string;
  value: unknown;
}

export interface RemoveConnectionPayload {
  id: string;
}

export interface AddSlotFlowPayload {
  blockId: string;
  /** Slot field name on the block (e.g. "cases" or "branches"). */
  field: string;
}

export interface RemoveSlotFlowPayload {
  blockId: string;
  field: string;
  flowId: string;
}

export interface SetFlowMetaPayload {
  flowId: string;
  /** Which per-entry metadata field to set on the sub-flow. */
  field: "when" | "description" | "inputSchema";
  value: string;
}

export interface SetEnvPayload {
  env: EnvVar[];
}

export interface LoadDocumentPayload {
  document: EditorDocument;
}

export interface SetIntegrationIdPayload {
  id: string;
}

export interface SetIntegrationTitlePayload {
  name: string;
}

export interface SetIntegrationFolderPayload {
  folderId: string | null;
}

export interface LoadIntegrationPayload {
  id: string;
  name: string;
  folderId: string | null;
  document: EditorDocument;
}
