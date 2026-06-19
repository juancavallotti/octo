import { defaultSourceSettings, mapFlow } from "@/app/model/document";
import type { EditorState } from "./reducer";
import type {
  AddSourcePayload,
  RemoveSourcePayload,
  SelectSourcePayload,
  UpdateSourceSettingPayload,
} from "./actions";

/**
 * Pure state transitions for a flow's source. Kept apart from handlers.ts so each
 * file stays small and focused (see docs/editor-coding-standards.md). A flow has
 * at most one source, so a source is identified by its flow's id.
 */

export function addSource(state: EditorState, p: AddSourcePayload): EditorState {
  const document = mapFlow(state.document, p.flowId, (flow) => ({
    ...flow,
    source: {
      connector: p.connector,
      type: p.type,
      settings: defaultSourceSettings(p.connector, p.type),
    },
  }));
  // Added → selected, so the settings panel opens on the new source.
  return {
    ...state,
    document,
    activeFlowId: p.flowId,
    selectedBlockId: null,
    selectedSourceFlowId: p.flowId,
    selectedConnectionId: null,
  };
}

export function selectSource(
  state: EditorState,
  p: SelectSourcePayload,
): EditorState {
  return {
    ...state,
    selectedSourceFlowId: p.flowId,
    selectedBlockId: null,
    selectedConnectionId: null,
    activeFlowId: p.flowId ?? state.activeFlowId,
  };
}

export function updateSourceSetting(
  state: EditorState,
  p: UpdateSourceSettingPayload,
): EditorState {
  const document = mapFlow(state.document, p.flowId, (flow) =>
    flow.source
      ? {
          ...flow,
          source: {
            ...flow.source,
            settings: { ...flow.source.settings, [p.field]: p.value },
          },
        }
      : flow,
  );
  return { ...state, document };
}

export function removeSource(
  state: EditorState,
  p: RemoveSourcePayload,
): EditorState {
  const document = mapFlow(state.document, p.flowId, (flow) => {
    const next = { ...flow };
    delete next.source;
    return next;
  });
  const selectedSourceFlowId =
    state.selectedSourceFlowId === p.flowId ? null : state.selectedSourceFlowId;
  return { ...state, document, selectedSourceFlowId };
}
