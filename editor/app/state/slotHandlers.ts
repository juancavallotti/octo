import {
  FlowDoc,
  emptyFlow,
  findBlock,
  findFlow,
  mapBlockById,
  mapFlow,
} from "@/app/model/document";
import { getBlockSpec } from "@/app/schema";
import type { EditorState } from "./reducer";
import type {
  AddSlotFlowPayload,
  RemoveSlotFlowPayload,
  SetFlowWhenPayload,
} from "./actions";

/**
 * Pure state transitions for a composite block's list slots — the switch's
 * `cases` and the fork's `branches`. Adding a slot flow grows the paths drawn on
 * the canvas (CompositeCard renders one SubFlow per slot entry); each case flow
 * carries a CEL `when` guard. Kept apart from handlers.ts to keep files focused.
 */

/** Append an empty sub-flow to a block's list slot (seeding `when` for cases). */
export function addSlotFlow(
  state: EditorState,
  p: AddSlotFlowPayload,
): EditorState {
  const document = mapBlockById(state.document, p.blockId, (block) => {
    const spec = getBlockSpec(block.type);
    const fieldSpec = spec?.fields.find((f) => f.name === p.field);
    const sub: FlowDoc = emptyFlow("");
    if (fieldSpec?.type === "case-list") sub.when = "";
    const slots = { ...(block.slots ?? {}) };
    slots[p.field] = [...(slots[p.field] ?? []), sub];
    return { ...block, slots };
  });
  return { ...state, document };
}

/** Remove one sub-flow from a block's list slot, dropping any dangling selection. */
export function removeSlotFlow(
  state: EditorState,
  p: RemoveSlotFlowPayload,
): EditorState {
  const document = mapBlockById(state.document, p.blockId, (block) => {
    const slots = { ...(block.slots ?? {}) };
    slots[p.field] = (slots[p.field] ?? []).filter((f) => f.id !== p.flowId);
    return { ...block, slots };
  });
  // The removed sub-flow (and its blocks) may have been selected/active.
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

/** Set a switch-case sub-flow's CEL `when` guard. */
export function setFlowWhen(
  state: EditorState,
  p: SetFlowWhenPayload,
): EditorState {
  const document = mapFlow(state.document, p.flowId, (flow) => ({
    ...flow,
    when: p.when,
  }));
  return { ...state, document };
}
