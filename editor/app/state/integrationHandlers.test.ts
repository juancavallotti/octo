import { describe, it, expect } from "vitest";
import { emptyDocument, newBlock } from "@/app/model/document";
import { EditorActionType } from "./actions";
import { initialState, reducer, type EditorState } from "./reducer";

/** A state carrying an unsaved integration with one flow and a block. */
function draftState(): EditorState {
  return reducer(initialState, {
    type: EditorActionType.ADD_BLOCK,
    data: { blockType: "log" },
  });
}

describe("integration metadata", () => {
  it("starts with empty integration metadata", () => {
    expect(initialState.integration).toEqual({
      id: null,
      name: "",
      folderId: null,
    });
  });

  it("sets the title without touching the document", () => {
    const state = draftState();
    const next = reducer(state, {
      type: EditorActionType.SET_INTEGRATION_TITLE,
      data: { name: "My Integration" },
    });
    expect(next.integration.name).toBe("My Integration");
    expect(next.document).toBe(state.document);
  });

  it("sets the folder", () => {
    const next = reducer(draftState(), {
      type: EditorActionType.SET_INTEGRATION_FOLDER,
      data: { folderId: "folder-1" },
    });
    expect(next.integration.folderId).toBe("folder-1");
  });

  it("records the persisted id after first save", () => {
    const next = reducer(draftState(), {
      type: EditorActionType.SET_INTEGRATION_ID,
      data: { id: "int-1" },
    });
    expect(next.integration.id).toBe("int-1");
  });

  it("loads an integration's document and metadata together", () => {
    const doc = emptyDocument();
    doc.flows[0].process = [newBlock("log")];
    const next = reducer(draftState(), {
      type: EditorActionType.LOAD_INTEGRATION,
      data: { id: "int-2", name: "Loaded", folderId: "f2", document: doc },
    });
    expect(next.integration).toEqual({
      id: "int-2",
      name: "Loaded",
      folderId: "f2",
    });
    expect(next.document).toBe(doc);
    expect(next.activeFlowId).toBe(doc.flows[0].id);
    expect(next.selectedBlockId).toBeNull();
  });

  it("resets metadata and document on NEW_INTEGRATION", () => {
    const loaded = reducer(draftState(), {
      type: EditorActionType.LOAD_INTEGRATION,
      data: {
        id: "int-3",
        name: "X",
        folderId: "f3",
        document: emptyDocument(),
      },
    });
    const next = reducer(loaded, { type: EditorActionType.NEW_INTEGRATION });
    expect(next.integration).toEqual({ id: null, name: "", folderId: null });
    expect(next.document.flows).toHaveLength(1);
  });
});
