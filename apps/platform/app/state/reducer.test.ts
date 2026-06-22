import { describe, it, expect } from "vitest";
import { emptyDocument, newBlock } from "@/app/model/document";
import { EditorActionType } from "./actions";
import { EditorState, initialState, reducer } from "./reducer";

function activeFlow(state: EditorState) {
  return state.document.flows.find((f) => f.id === state.activeFlowId)!;
}

describe("editor reducer", () => {
  it("starts empty with no flows", () => {
    expect(initialState.document.flows).toHaveLength(0);
    expect(initialState.activeFlowId).toBeNull();
  });

  it("auto-creates a flow for the first added block and selects it", () => {
    const next = reducer(initialState, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log" },
    });
    expect(next.document.flows).toHaveLength(1);
    const process = activeFlow(next).process;
    expect(process).toHaveLength(1);
    expect(process[0].type).toBe("log");
    expect(next.selectedBlockId).toBe(process[0].id);
  });

  it("inserts a block at a given index", () => {
    let state = reducer(initialState, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log" },
    });
    state = reducer(state, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "sql", index: 0 },
    });
    expect(activeFlow(state).process.map((b) => b.type)).toEqual(["sql", "log"]);
  });

  it("seeds block settings from schema defaults", () => {
    const next = reducer(initialState, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log" },
    });
    expect(activeFlow(next).process[0].settings.level).toBe("info");
  });

  it("reorders blocks", () => {
    let state = initialState;
    for (const t of ["log", "sql", "rest"]) {
      state = reducer(state, {
        type: EditorActionType.ADD_BLOCK,
        data: { blockType: t },
      });
    }
    state = reducer(state, {
      type: EditorActionType.MOVE_BLOCK,
      data: { flowId: state.activeFlowId, fromIndex: 0, toIndex: 2 },
    });
    expect(activeFlow(state).process.map((b) => b.type)).toEqual([
      "sql",
      "rest",
      "log",
    ]);
  });

  it("removes a block and clears its selection", () => {
    const added = reducer(initialState, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log" },
    });
    const blockId = activeFlow(added).process[0].id;
    const next = reducer(added, {
      type: EditorActionType.REMOVE_BLOCK,
      data: { flowId: added.activeFlowId, blockId },
    });
    expect(activeFlow(next).process).toHaveLength(0);
    expect(next.selectedBlockId).toBeNull();
  });

  it("adds a flow and makes it active", () => {
    const next = reducer(initialState, { type: EditorActionType.ADD_FLOW });
    expect(next.document.flows).toHaveLength(1);
    expect(next.activeFlowId).toBe(next.document.flows[0].id);
  });

  it("adds blocks to the active flow by default", () => {
    let state = reducer(initialState, { type: EditorActionType.ADD_FLOW });
    state = reducer(state, { type: EditorActionType.ADD_FLOW });
    const next = reducer(state, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log" },
    });
    expect(next.document.flows[0].process).toHaveLength(0);
    expect(next.document.flows[1].process).toHaveLength(1);
  });

  it("adds a block to a specific flow when flowId is given", () => {
    let state = reducer(initialState, { type: EditorActionType.ADD_FLOW });
    state = reducer(state, { type: EditorActionType.ADD_FLOW });
    const firstId = state.document.flows[0].id;
    const next = reducer(state, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log", flowId: firstId },
    });
    expect(next.document.flows[0].process).toHaveLength(1);
    expect(next.document.flows[1].process).toHaveLength(0);
    expect(next.activeFlowId).toBe(firstId);
  });

  it("adds a typed source to a flow and selects it", () => {
    const withFlow = reducer(initialState, { type: EditorActionType.ADD_FLOW });
    const flowId = withFlow.document.flows[0].id;
    expect(withFlow.document.flows[0].source).toBeUndefined();
    const next = reducer(withFlow, {
      type: EditorActionType.ADD_SOURCE,
      data: { flowId, connector: "cron", type: "cron" },
    });
    expect(next.document.flows[0].source).toMatchObject({
      connector: "cron",
      type: "cron",
    });
    expect(next.selectedSourceFlowId).toBe(flowId);
    expect(next.selectedBlockId).toBeNull();
  });

  it("updates and removes a flow's source", () => {
    const withFlow = reducer(initialState, { type: EditorActionType.ADD_FLOW });
    const flowId = withFlow.document.flows[0].id;
    const withSource = reducer(withFlow, {
      type: EditorActionType.ADD_SOURCE,
      data: { flowId, connector: "cron", type: "cron" },
    });
    const updated = reducer(withSource, {
      type: EditorActionType.UPDATE_SOURCE_SETTING,
      data: { flowId, field: "schedule", value: "@every 2s" },
    });
    expect(updated.document.flows[0].source?.settings.schedule).toBe(
      "@every 2s",
    );
    const removed = reducer(updated, {
      type: EditorActionType.REMOVE_SOURCE,
      data: { flowId },
    });
    expect(removed.document.flows[0].source).toBeUndefined();
    expect(removed.selectedSourceFlowId).toBeNull();
  });

  it("adds a block into a nested composite sub-flow", () => {
    const withIf = reducer(initialState, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "if" },
    });
    const thenFlowId = activeFlow(withIf).process[0].slots!.then[0].id;
    const next = reducer(withIf, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log", flowId: thenFlowId },
    });
    const thenFlow = next.document.flows[0].process[0].slots!.then[0];
    expect(thenFlow.process.map((b) => b.type)).toEqual(["log"]);
  });

  it("moves a block from a flow into a nested sub-flow", () => {
    let state = reducer(initialState, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "if" },
    });
    state = reducer(state, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log" },
    });
    const root = activeFlow(state);
    const log = root.process.find((b) => b.type === "log")!;
    const ifBlock = root.process.find((b) => b.type === "if")!;
    const thenFlowId = ifBlock.slots!.then[0].id;

    const next = reducer(state, {
      type: EditorActionType.MOVE_BLOCK_ACROSS,
      data: { fromFlowId: root.id, toFlowId: thenFlowId, blockId: log.id },
    });
    const nextRoot = next.document.flows[0];
    expect(nextRoot.process.map((b) => b.type)).toEqual(["if"]);
    const movedInto = nextRoot.process[0].slots!.then[0];
    expect(movedInto.process[0].id).toBe(log.id);
    expect(next.selectedBlockId).toBe(log.id);
  });

  it("loads a document and activates its first flow", () => {
    const doc = emptyDocument();
    doc.flows[0].name = "imported";
    doc.flows[0].process = [newBlock("log")];
    const next = reducer(initialState, {
      type: EditorActionType.LOAD_DOCUMENT,
      data: { document: doc },
    });
    expect(next.activeFlowId).toBe(doc.flows[0].id);
    expect(activeFlow(next).name).toBe("imported");
    expect(activeFlow(next).process).toHaveLength(1);
  });

  it("does not mutate the previous state", () => {
    const next = reducer(initialState, {
      type: EditorActionType.ADD_BLOCK,
      data: { blockType: "log" },
    });
    expect(initialState.document.flows).toHaveLength(0);
    expect(next).not.toBe(initialState);
  });
});
