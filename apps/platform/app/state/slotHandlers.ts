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
  SetFlowMetaPayload,
} from "./actions";

/**
 * Pure state transitions for a composite block's list slots — the switch's
 * `cases` and the fork's `branches`. Adding a slot flow grows the paths drawn on
 * the canvas (CompositeCard renders one SubFlow per slot entry); each case flow
 * carries a CEL `when` guard. Kept apart from handlers.ts to keep files focused.
 */

/**
 * Starter JSON Schema seeded into a new ai-agent tool's `inputSchema`, so the
 * user edits a working object-schema template instead of an empty box. The tool's
 * arguments arrive as the message body, so these properties become `body.<name>`
 * inside the tool's steps.
 */
const DEFAULT_TOOL_INPUT_SCHEMA = `{
  "type": "object",
  "properties": {
    "param": { "type": "string", "description": "Describe this parameter." }
  },
  "required": ["param"]
}`;

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
    else if (fieldSpec?.type === "route-list") sub.description = "";
    else if (fieldSpec?.type === "tool-list") {
      sub.description = "";
      sub.inputSchema = DEFAULT_TOOL_INPUT_SCHEMA;
    }
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

/**
 * Set one per-entry metadata field on a sub-flow: a switch-case's CEL `when`
 * guard, an ai-router route / ai-agent tool's `description`, or a tool's
 * `inputSchema`. The steps inside the sub-flow are edited on the canvas.
 */
export function setFlowMeta(
  state: EditorState,
  p: SetFlowMetaPayload,
): EditorState {
  const document = mapFlow(state.document, p.flowId, (flow) => ({
    ...flow,
    [p.field]: p.value,
  }));
  return { ...state, document };
}
