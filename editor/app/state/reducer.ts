import { ReducerAction } from "@eetr/react-reducer-utils";
import {
  EditorDocument,
  FlowDoc,
  emptyDocument,
  emptyFlow,
  findFlow,
  mapFlow,
  newBlock,
} from "@/app/model/document";
import {
  AddBlockPayload,
  EditorActionType,
  LoadDocumentPayload,
  MoveBlockAcrossPayload,
  MoveBlockPayload,
  RemoveBlockPayload,
  SelectBlockPayload,
  SetActiveFlowPayload,
} from "./actions";

/**
 * Editor-wide state. EditorShell is a "large" component, so its state lives in a
 * reducer (per the coding standards). The document is the in-memory editing model
 * (see app/model/document.ts); a file holds many flows, all editable at once.
 * `activeFlowId` is just the target for click-to-add and selection highlighting.
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
  const document = emptyDocument();
  return {
    document,
    activeFlowId: document.flows[0]?.id ?? null,
    selectedBlockId: null,
    selectedComponentId: null,
  };
}

export const initialState: EditorState = makeInitialState();

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

function addFlow(state: EditorState): EditorState {
  const flow = emptyFlow(`Flow ${state.document.flows.length + 1}`);
  return {
    ...state,
    document: { ...state.document, flows: [...state.document.flows, flow] },
    activeFlowId: flow.id,
    selectedBlockId: null,
  };
}

function addBlock(state: EditorState, p: AddBlockPayload): EditorState {
  const flowId = p.flowId ?? state.activeFlowId;
  const block = newBlock(p.blockType);
  const document = updateFlow(state, flowId, (flow) => {
    const process = flow.process.slice();
    process.splice(p.index ?? process.length, 0, block);
    return { ...flow, process };
  });
  return { ...state, document, activeFlowId: flowId, selectedBlockId: block.id };
}

function moveBlock(state: EditorState, p: MoveBlockPayload): EditorState {
  const document = updateFlow(state, p.flowId, (flow) => ({
    ...flow,
    process: arrayMove(flow.process, p.fromIndex, p.toIndex),
  }));
  return { ...state, document };
}

function moveBlockAcross(
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
  return { ...state, document, activeFlowId: p.toFlowId, selectedBlockId: block.id };
}

function removeBlock(state: EditorState, p: RemoveBlockPayload): EditorState {
  const document = updateFlow(state, p.flowId, (flow) => ({
    ...flow,
    process: flow.process.filter((b) => b.id !== p.blockId),
  }));
  const selectedBlockId =
    state.selectedBlockId === p.blockId ? null : state.selectedBlockId;
  return { ...state, document, selectedBlockId };
}

function loadDocument(state: EditorState, p: LoadDocumentPayload): EditorState {
  return {
    ...state,
    document: p.document,
    activeFlowId: p.document.flows[0]?.id ?? null,
    selectedBlockId: null,
  };
}

export function reducer(
  state: EditorState = initialState,
  action: ReducerAction<EditorActionType>,
): EditorState {
  switch (action.type) {
    case EditorActionType.ADD_FLOW:
      return addFlow(state);
    case EditorActionType.ADD_BLOCK:
      return addBlock(state, action.data as AddBlockPayload);
    case EditorActionType.MOVE_BLOCK:
      return moveBlock(state, action.data as MoveBlockPayload);
    case EditorActionType.MOVE_BLOCK_ACROSS:
      return moveBlockAcross(state, action.data as MoveBlockAcrossPayload);
    case EditorActionType.REMOVE_BLOCK:
      return removeBlock(state, action.data as RemoveBlockPayload);
    case EditorActionType.SELECT_BLOCK:
      return {
        ...state,
        selectedBlockId: (action.data as SelectBlockPayload).blockId,
      };
    case EditorActionType.SET_ACTIVE_FLOW:
      return {
        ...state,
        activeFlowId: (action.data as SetActiveFlowPayload).flowId,
        selectedBlockId: null,
      };
    case EditorActionType.LOAD_DOCUMENT:
      return loadDocument(state, action.data as LoadDocumentPayload);
    case EditorActionType.SELECT_COMPONENT:
      return { ...state, selectedComponentId: action.data as string };
    case EditorActionType.CLEAR_SELECTION:
      return { ...state, selectedComponentId: null };
    default:
      return state;
  }
}
