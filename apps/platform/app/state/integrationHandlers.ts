import { emptyDocument } from "@/app/model/document";
import type { EditorState } from "./reducer";
import type {
  LoadIntegrationPayload,
  SetIntegrationFolderPayload,
  SetIntegrationIdPayload,
  SetIntegrationTitlePayload,
} from "./actions";

/**
 * Pure state transitions for the "current integration" metadata: the title,
 * folder membership, and persisted id that the title bar reads and the Save
 * button writes. Kept apart from the document handlers so each file stays small
 * (see docs/editor-coding-standards.md).
 */

export function setIntegrationId(
  state: EditorState,
  p: SetIntegrationIdPayload,
): EditorState {
  return {
    ...state,
    integration: { ...state.integration, id: p.id },
  };
}

export function setIntegrationTitle(
  state: EditorState,
  p: SetIntegrationTitlePayload,
): EditorState {
  return {
    ...state,
    integration: { ...state.integration, name: p.name },
  };
}

export function setIntegrationFolder(
  state: EditorState,
  p: SetIntegrationFolderPayload,
): EditorState {
  return {
    ...state,
    integration: { ...state.integration, folderId: p.folderId },
  };
}

/** Load a saved integration: swap in its document and metadata together. */
export function loadIntegration(
  state: EditorState,
  p: LoadIntegrationPayload,
): EditorState {
  return {
    ...state,
    document: p.document,
    activeFlowId: p.document.flows[0]?.id ?? null,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
    integration: { id: p.id, name: p.name, folderId: p.folderId },
  };
}

/** Reset to a fresh, unsaved integration (a blank canvas with no metadata). */
export function newIntegration(state: EditorState): EditorState {
  const document = emptyDocument();
  return {
    ...state,
    document,
    activeFlowId: document.flows[0]?.id ?? null,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
    integration: { id: null, name: "", folderId: null },
  };
}
