import { newConnector } from "../model/connectors";
import type { ConnectorInstance } from "../model/document";
import type { EditorState } from "./reducer";
import type {
  AddConnectionPayload,
  RemoveConnectionPayload,
  RenameConnectionPayload,
  SelectConnectionPayload,
  UpdateConnectionSettingPayload,
} from "./actions";

/**
 * Pure state transitions for document-global connector instances
 * ("connections"). Kept apart from handlers.ts so each file stays small (see
 * docs/editor-coding-standards.md). Connections live on `document.connectors`
 * and are referenced by their unique, slug-style name.
 */

export function addConnection(
  state: EditorState,
  p: AddConnectionPayload,
): EditorState {
  const taken = new Set(state.document.connectors.map((c) => c.name));
  const connector = newConnector(p.type, taken);
  // Added → selected, so the settings panel opens on the new connection.
  return {
    ...state,
    document: {
      ...state.document,
      connectors: [...state.document.connectors, connector],
    },
    selectedConnectionId: connector.id,
    selectedBlockId: null,
    selectedSourceFlowId: null,
  };
}

export function selectConnection(
  state: EditorState,
  p: SelectConnectionPayload,
): EditorState {
  return {
    ...state,
    selectedConnectionId: p.id,
    selectedBlockId: null,
    selectedSourceFlowId: null,
  };
}

export function renameConnection(
  state: EditorState,
  p: RenameConnectionPayload,
): EditorState {
  const connectors = state.document.connectors.map((c) =>
    c.id === p.id ? { ...c, name: p.name } : c,
  );
  return { ...state, document: { ...state.document, connectors } };
}

/**
 * Set one setting of the connection with id `p.id`, in whichever file holds it. A
 * connection outside the open file is changed in that file's entry, which is its
 * present (only the open file's entry is stale).
 */
export function updateConnectionSetting(
  state: EditorState,
  p: UpdateConnectionSettingPayload,
): EditorState {
  const set = (connectors: ConnectorInstance[]) =>
    connectors.map((c) =>
      c.id === p.id
        ? { ...c, settings: { ...c.settings, [p.field]: p.value } }
        : c,
    );
  const holds = (connectors: ConnectorInstance[]) => connectors.some((c) => c.id === p.id);
  if (holds(state.document.connectors)) {
    return { ...state, document: { ...state.document, connectors: set(state.document.connectors) } };
  }
  return {
    ...state,
    files: state.files.map((f) =>
      f.path !== state.activePath && holds(f.document.connectors)
        ? { ...f, document: { ...f.document, connectors: set(f.document.connectors) } }
        : f,
    ),
  };
}

export function removeConnection(
  state: EditorState,
  p: RemoveConnectionPayload,
): EditorState {
  const connectors = state.document.connectors.filter((c) => c.id !== p.id);
  const selectedConnectionId =
    state.selectedConnectionId === p.id ? null : state.selectedConnectionId;
  return {
    ...state,
    document: { ...state.document, connectors },
    selectedConnectionId,
  };
}
