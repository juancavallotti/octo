import {
  EditorDocument,
  FlowDoc,
  emptyFlow,
  findBlock,
  findFlow,
  mapBlockById,
  mapFlow,
  newBlock,
  withErrorChain,
} from "@/app/model/document";
import type { EditorState } from "./reducer";
import type {
  AddBlockPayload,
  LoadDocumentPayload,
  MoveBlockAcrossPayload,
  MoveBlockPayload,
  RemoveBlockPayload,
  RemoveFlowPayload,
  RenameBlockPayload,
  RenameFlowPayload,
  UpdateBlockSettingPayload,
} from "./actions";

/**
 * Pure state transitions for the editor reducer. Each takes the current state
 * (plus an action payload) and returns the next state immutably. They live here,
 * apart from the reducer switch in reducer.ts, to keep each file small and
 * focused (see docs/editor-coding-standards.md).
 */

/** Immutable move of one array element from one index to another. */
function arrayMove<T>(items: T[], from: number, to: number): T[] {
  const next = items.slice();
  const [moved] = next.splice(from, 1);
  next.splice(to, 0, moved);
  return next;
}

/** Apply `fn` to one flow by id (at any depth), returning a new document. */
function updateFlow(
  state: EditorState,
  flowId: string | null,
  fn: (flow: FlowDoc) => FlowDoc,
): EditorDocument {
  if (!flowId) return state.document;
  return mapFlow(state.document, flowId, fn);
}

export function addFlow(state: EditorState): EditorState {
  const flow = withErrorChain(emptyFlow(`flow-${state.document.flows.length + 1}`));
  return {
    ...state,
    document: { ...state.document, flows: [...state.document.flows, flow] },
    activeFlowId: flow.id,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
  };
}

export function addBlock(state: EditorState, p: AddBlockPayload): EditorState {
  const block = newBlock(p.blockType);
  const flowId = p.flowId ?? state.activeFlowId;

  // No target flow yet (scratch document) — create one and start it with this block.
  if (!flowId || !findFlow(state.document, flowId)) {
    const flow = withErrorChain(emptyFlow(`flow-${state.document.flows.length + 1}`));
    flow.process = [block];
    const document = {
      ...state.document,
      flows: [...state.document.flows, flow],
    };
    return {
      ...state,
      document,
      activeFlowId: flow.id,
      selectedBlockId: block.id,
      selectedSourceFlowId: null,
      selectedConnectionId: null,
    };
  }

  const document = updateFlow(state, flowId, (flow) => {
    const process = flow.process.slice();
    process.splice(p.index ?? process.length, 0, block);
    return { ...flow, process };
  });
  return {
    ...state,
    document,
    activeFlowId: flowId,
    selectedBlockId: block.id,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
  };
}

export function moveBlock(state: EditorState, p: MoveBlockPayload): EditorState {
  const document = updateFlow(state, p.flowId, (flow) => ({
    ...flow,
    process: arrayMove(flow.process, p.fromIndex, p.toIndex),
  }));
  return { ...state, document };
}

export function moveBlockAcross(
  state: EditorState,
  p: MoveBlockAcrossPayload,
): EditorState {
  if (p.fromFlowId === p.toFlowId) return state;
  const fromFlow = findFlow(state.document, p.fromFlowId);
  const block = fromFlow?.process.find((b) => b.id === p.blockId);
  if (!block) return state;

  const withoutBlock = mapFlow(state.document, p.fromFlowId, (flow) => ({
    ...flow,
    process: flow.process.filter((b) => b.id !== p.blockId),
  }));
  const document = mapFlow(withoutBlock, p.toFlowId, (flow) => {
    const process = flow.process.slice();
    process.splice(p.index ?? process.length, 0, block);
    return { ...flow, process };
  });
  return {
    ...state,
    document,
    activeFlowId: p.toFlowId,
    selectedBlockId: block.id,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
  };
}

export function removeBlock(
  state: EditorState,
  p: RemoveBlockPayload,
): EditorState {
  const document = updateFlow(state, p.flowId, (flow) => ({
    ...flow,
    process: flow.process.filter((b) => b.id !== p.blockId),
  }));
  const selectedBlockId =
    state.selectedBlockId === p.blockId ? null : state.selectedBlockId;
  return { ...state, document, selectedBlockId };
}

export function removeFlow(
  state: EditorState,
  p: RemoveFlowPayload,
): EditorState {
  const document = {
    ...state.document,
    flows: state.document.flows.filter((f) => f.id !== p.flowId),
  };
  // Drop active/selection pointers that no longer resolve in the new document.
  const activeFlowId =
    state.activeFlowId && findFlow(document, state.activeFlowId)
      ? state.activeFlowId
      : null;
  const selectedBlockId =
    state.selectedBlockId && findBlock(document, state.selectedBlockId)
      ? state.selectedBlockId
      : null;
  const selectedSourceFlowId =
    state.selectedSourceFlowId && findFlow(document, state.selectedSourceFlowId)
      ? state.selectedSourceFlowId
      : null;
  return {
    ...state,
    document,
    activeFlowId,
    selectedBlockId,
    selectedSourceFlowId,
  };
}

export function renameFlow(
  state: EditorState,
  p: RenameFlowPayload,
): EditorState {
  const document = mapFlow(state.document, p.flowId, (flow) => ({
    ...flow,
    name: p.name,
  }));
  return { ...state, document };
}

export function updateBlockSetting(
  state: EditorState,
  p: UpdateBlockSettingPayload,
): EditorState {
  const document = mapBlockById(state.document, p.blockId, (block) => ({
    ...block,
    settings: { ...block.settings, [p.field]: p.value },
  }));
  return { ...state, document };
}

export function renameBlock(
  state: EditorState,
  p: RenameBlockPayload,
): EditorState {
  const name = p.name.trim();
  const document = mapBlockById(state.document, p.blockId, (block) => {
    if (name) return { ...block, name };
    // Keep `name` optional: drop the key when cleared.
    const next = { ...block };
    delete next.name;
    return next;
  });
  return { ...state, document };
}

export function loadDocument(
  state: EditorState,
  p: LoadDocumentPayload,
): EditorState {
  return {
    ...state,
    document: p.document,
    activeFlowId: p.document.flows[0]?.id ?? null,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
  };
}
