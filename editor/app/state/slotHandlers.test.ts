import { describe, it, expect } from "vitest";
import {
  blankDocument,
  emptyFlow,
  findBlock,
  newBlock,
} from "@/app/model/document";
import type { EditorState } from "./reducer";
import { addSlotFlow, removeSlotFlow, setFlowWhen } from "./slotHandlers";

/** A state holding one flow with a single switch block (seeded with one case). */
function stateWithSwitch(): { state: EditorState; blockId: string } {
  const sw = newBlock("switch");
  const flow = emptyFlow("main");
  flow.process = [sw];
  const state: EditorState = {
    document: { ...blankDocument(), flows: [flow] },
    activeFlowId: flow.id,
    selectedBlockId: null,
    selectedSourceFlowId: null,
    selectedConnectionId: null,
    selectedComponentId: null,
  };
  return { state, blockId: sw.id };
}

const cases = (s: EditorState, id: string) =>
  findBlock(s.document, id)!.slots!.cases;

describe("slotHandlers", () => {
  it("appends a case seeded with an empty `when`", () => {
    const { state, blockId } = stateWithSwitch();
    const before = cases(state, blockId).length;

    const next = addSlotFlow(state, { blockId, field: "cases" });
    const after = cases(next, blockId);
    expect(after).toHaveLength(before + 1);
    expect(after[after.length - 1].when).toBe("");
  });

  it("sets a case's CEL `when` guard", () => {
    const { state, blockId } = stateWithSwitch();
    const caseFlow = cases(state, blockId)[0];

    const next = setFlowWhen(state, { flowId: caseFlow.id, when: "vars.x == 1" });
    expect(cases(next, blockId)[0].when).toBe("vars.x == 1");
  });

  it("removes a case and clears selection pointing into it", () => {
    const { state, blockId } = stateWithSwitch();
    let s = addSlotFlow(state, { blockId, field: "cases" });
    const added = cases(s, blockId)[1];
    s = { ...s, activeFlowId: added.id, selectedSourceFlowId: added.id };

    const next = removeSlotFlow(s, {
      blockId,
      field: "cases",
      flowId: added.id,
    });
    expect(cases(next, blockId)).toHaveLength(1);
    expect(next.activeFlowId).toBeNull();
    expect(next.selectedSourceFlowId).toBeNull();
  });
});
