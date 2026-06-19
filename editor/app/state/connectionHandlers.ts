import { newConnector } from "@/app/model/connectors";
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

export function updateConnectionSetting(
  state: EditorState,
  p: UpdateConnectionSettingPayload,
): EditorState {
  const connectors = state.document.connectors.map((c) =>
    c.id === p.id
      ? { ...c, settings: { ...c.settings, [p.field]: p.value } }
      : c,
  );
  return { ...state, document: { ...state.document, connectors } };
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
